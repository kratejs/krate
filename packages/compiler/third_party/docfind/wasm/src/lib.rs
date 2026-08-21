//! docfind — WASM document search engine (Microsoft), embedded into krate.
//!
//! This is the *search* module: it is served to the browser and, once an index
//! has been embedded into it (see the `builder` crate), answers full-text /
//! fuzzy search queries entirely on the client. It exposes a plain C-ABI (no
//! wasm-bindgen) so the JS glue is a small hand-written file.
//!
//! The `INDEX_BASE` / `INDEX_LEN` globals are patched by `docfind_build` (in
//! the builder crate) when an index is embedded. They must remain `static mut`
//! so the linker emits them as `i32.const` globals backed by data-segment
//! storage the embedder can rewrite.
//!
//! ABI:
//! - `docfind_search(query_ptr, query_len, max_results, out_ptr, out_len)
//!   -> i32` — search the embedded index; on success writes an allocated buffer
//!   containing a JSON array of `{title, category, href, body}`.
//! - `docfind_alloc(len) -> ptr` / `docfind_free(ptr, len)` — allocate/free
//!   input buffers in WASM memory.

use std::alloc::{alloc, dealloc, Layout};
use std::sync::OnceLock;

use docfind_core::Index;

#[global_allocator]
static ALLOC: dlmalloc::GlobalDlmalloc = dlmalloc::GlobalDlmalloc;

#[no_mangle]
pub static mut INDEX_BASE: u32 = 0xdead_beef;

#[no_mangle]
pub static mut INDEX_LEN: u32 = 0xdead_beef;

static INDEX: OnceLock<Index> = OnceLock::new();

// ---------------------------------------------------------------------------
// Memory helpers
// ---------------------------------------------------------------------------

/// Allocate a zero-initialized buffer of `len` bytes (aligned to 1).
#[no_mangle]
pub extern "C" fn docfind_alloc(len: usize) -> *mut u8 {
    let layout = Layout::from_size_align(len.max(1), 1).expect("invalid allocation size");
    unsafe { alloc(layout) }
}

/// Free a buffer previously returned by `docfind_alloc` / `docfind_search`.
#[no_mangle]
pub extern "C" fn docfind_free(ptr: *mut u8, len: usize) {
    if ptr.is_null() {
        return;
    }
    let layout = Layout::from_size_align(len.max(1), 1).expect("invalid deallocation size");
    unsafe { dealloc(ptr, layout) }
}

unsafe fn input_slice<'a>(ptr: *const u8, len: usize) -> &'a [u8] {
    if len == 0 {
        return &[];
    }
    unsafe { std::slice::from_raw_parts(ptr, len) }
}

/// Copy `bytes` into a fresh heap buffer and publish (ptr, len) through the
/// out pointers. The caller must free with `docfind_free`.
fn write_output(bytes: Vec<u8>, out_ptr: *mut usize, out_len: *mut usize) {
    let len = bytes.len();
    let ptr = if len == 0 {
        std::ptr::null_mut()
    } else {
        docfind_alloc(len)
    };
    unsafe {
        if len > 0 {
            std::ptr::copy_nonoverlapping(bytes.as_ptr(), ptr, len);
        }
        if !out_ptr.is_null() {
            *out_ptr = ptr as usize;
        }
        if !out_len.is_null() {
            *out_len = len;
        }
    }
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

/// Search the embedded index for `query`. Returns a JSON array of matching
/// documents (`{title, category, href, body}`) in an allocated buffer.
#[no_mangle]
pub extern "C" fn docfind_search(
    query_ptr: *const u8,
    query_len: usize,
    max_results: usize,
    out_ptr: *mut usize,
    out_len: *mut usize,
) -> i32 {
    let result = (|| -> Result<Vec<u8>, String> {
        let query = std::str::from_utf8(unsafe { input_slice(query_ptr, query_len) })
            .map_err(|e| e.to_string())?;
        let index = INDEX.get_or_init(|| {
            let base = unsafe { INDEX_BASE as usize as *const u8 };
            let len = unsafe { INDEX_LEN as usize };
            let bytes = unsafe { std::slice::from_raw_parts(base, len) };
            Index::from_bytes(bytes).expect("failed to deserialize embedded docfind index")
        });
        let results = docfind_core::search(index, query, max_results).map_err(|e| e.to_string())?;
        serde_json::to_vec(&results).map_err(|e| e.to_string())
    })();

    match result {
        Ok(bytes) => {
            write_output(bytes, out_ptr, out_len);
            0
        }
        Err(_) => 1,
    }
}
