//! docfind builder module.
//!
//! Builds a search index from a JSON array of documents and embeds it into the
//! `wasm` crate's search module (which krate passes in as a template), producing
//! the final `docfind_bg.wasm` served to the browser. This runs in-process from
//! Go at docs-build time via a WebAssembly runtime — no subprocess, no temp
//! JSON file. The embedding logic is a port of the upstream `docfind` CLI.
//!
//! ABI:
//! - `docfind_build(docs_json_ptr, docs_json_len, tpl_ptr, tpl_len,
//!   out_ptr, out_len) -> i32` — build + embed; on success writes an allocated
//!   buffer containing the final WASM module (free with `docfind_free`).
//! - `docfind_alloc(len) -> ptr` / `docfind_free(ptr, len)` — memory helpers.

use std::alloc::{alloc, dealloc, Layout};
use std::collections::HashMap;

use docfind_core::{build_index, Document};
use wasm_encoder::{ConstExpr, DataSection, MemorySection, MemoryType};
use wasmparser::{Parser, Payload};

#[global_allocator]
static ALLOC: dlmalloc::GlobalDlmalloc = dlmalloc::GlobalDlmalloc;

// ---------------------------------------------------------------------------
// Memory helpers
// ---------------------------------------------------------------------------

/// Allocate a zero-initialized buffer of `len` bytes (aligned to 1).
#[no_mangle]
pub extern "C" fn docfind_alloc(len: usize) -> *mut u8 {
    let layout = Layout::from_size_align(len.max(1), 1).expect("invalid allocation size");
    unsafe { alloc(layout) }
}

/// Free a buffer previously returned by `docfind_alloc` / `docfind_build`.
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
// Index embedding (moved from the upstream `cli` crate)
// ---------------------------------------------------------------------------

#[derive(Debug)]
enum WasmDataSegment {
    Passive(Vec<u8>),
    Active {
        memory_index: u32,
        offset: ConstExpr,
        data: Vec<u8>,
        i32const_offset: Option<i32>,
    },
}

#[derive(Debug)]
enum WasmSection {
    Data(Vec<WasmDataSegment>),
    DataCount(u32),
    Memory,
    Raw { id: u8, data: Vec<u8> },
}

fn convert_const_expr(expr: &wasmparser::ConstExpr) -> Result<ConstExpr, String> {
    let mut ops_reader = expr.get_operators_reader();
    if !ops_reader.eof() {
        let op = ops_reader.read().map_err(|e| e.to_string())?;
        match op {
            wasmparser::Operator::I32Const { value } => return Ok(ConstExpr::i32_const(value)),
            wasmparser::Operator::I64Const { value } => return Ok(ConstExpr::i64_const(value)),
            wasmparser::Operator::F32Const { value } => {
                let f32_val = f32::from_bits(value.bits());
                return Ok(ConstExpr::f32_const(f32_val.into()));
            }
            wasmparser::Operator::F64Const { value } => {
                let f64_val = f64::from_bits(value.bits());
                return Ok(ConstExpr::f64_const(f64_val.into()));
            }
            wasmparser::Operator::GlobalGet { global_index } => {
                return Ok(ConstExpr::global_get(global_index));
            }
            wasmparser::Operator::RefNull { .. } => {
                return Ok(ConstExpr::ref_null(wasm_encoder::HeapType::Abstract {
                    shared: false,
                    ty: wasm_encoder::AbstractHeapType::Func,
                }));
            }
            wasmparser::Operator::RefFunc { function_index } => {
                return Ok(ConstExpr::ref_func(function_index));
            }
            _ => return Ok(ConstExpr::raw(vec![])),
        }
    }
    Ok(ConstExpr::raw(vec![]))
}

/// Embed `raw_index` (postcard-encoded) into `template` (the search module
/// built from the `wasm` crate) by patching the `INDEX_BASE`/`INDEX_LEN`
/// globals, appending a data segment with the index, and growing memory.
fn embed_index(template: &[u8], raw_index: &[u8]) -> Result<Vec<u8>, String> {
    let mut sections: Vec<WasmSection> = Vec::new();

    let mut old_memory_page_count: u64 = 0;
    let mut index_base_global_index: Option<u32> = None;
    let mut index_len_global_index: Option<u32> = None;
    let mut i32_globals: HashMap<u32, i32> = HashMap::new();

    for payload in Parser::new(0).parse_all(template) {
        let payload = payload.map_err(|e| e.to_string())?;

        if let Payload::DataSection(reader) = payload {
            let mut data_segments: Vec<WasmDataSegment> = Vec::new();
            for data in reader {
                let data = data.map_err(|e| e.to_string())?;
                match data.kind {
                    wasmparser::DataKind::Passive => {
                        data_segments.push(WasmDataSegment::Passive(data.data.to_vec()));
                    }
                    wasmparser::DataKind::Active {
                        memory_index,
                        offset_expr,
                    } => {
                        let const_expr = convert_const_expr(&offset_expr)?;
                        let i32const_offset = if let Ok(wasmparser::Operator::I32Const { value }) =
                            offset_expr.get_operators_reader().read()
                        {
                            Some(value)
                        } else {
                            None
                        };
                        data_segments.push(WasmDataSegment::Active {
                            memory_index,
                            offset: const_expr,
                            data: data.data.to_vec(),
                            i32const_offset,
                        });
                    }
                }
            }
            sections.push(WasmSection::Data(data_segments));
        } else if let Payload::DataCountSection { count, .. } = payload {
            sections.push(WasmSection::DataCount(count));
        } else if let Payload::MemorySection(reader) = payload {
            for memory in reader {
                old_memory_page_count = memory.map_err(|e| e.to_string())?.initial as u64;
            }
            sections.push(WasmSection::Memory);
        } else {
            if let Some((id, data)) = payload.as_section() {
                sections.push(WasmSection::Raw {
                    id,
                    data: template[data.start..data.end].to_vec(),
                });
            }

            match payload {
                Payload::ExportSection(reader) => {
                    for export in reader {
                        let export = export.map_err(|e| e.to_string())?;
                        if export.name == "INDEX_BASE" {
                            index_base_global_index = Some(export.index);
                        } else if export.name == "INDEX_LEN" {
                            index_len_global_index = Some(export.index);
                        }
                    }
                }
                Payload::GlobalSection(reader) => {
                    for (idx, global) in reader.into_iter().enumerate() {
                        let global = global.map_err(|e| e.to_string())?;
                        let mut ops_reader = global.init_expr.get_operators_reader();
                        if !ops_reader.eof() {
                            if let Ok(wasmparser::Operator::I32Const { value }) = ops_reader.read()
                            {
                                i32_globals.insert(idx as u32, value);
                            }
                        }
                    }
                }
                _ => {}
            }
        }
    }

    let index_base_global_index =
        index_base_global_index.ok_or("Could not find INDEX_BASE global index")?;
    let index_len_global_index =
        index_len_global_index.ok_or("Could not find INDEX_LEN global index")?;

    let index_base_global_address = i32_globals
        .get(&index_base_global_index)
        .ok_or("Could not find INDEX_BASE global value")?;
    let index_len_global_address = i32_globals
        .get(&index_len_global_index)
        .ok_or("Could not find INDEX_LEN global value")?;

    let new_memory_page_count = old_memory_page_count + (raw_index.len() as u64 / 0x10000) + 1;
    let index_base = old_memory_page_count * 0x10000;

    let mut encoder = wasm_encoder::Module::new();

    for section in sections {
        match section {
            WasmSection::DataCount(count) => {
                encoder.section(&wasm_encoder::DataCountSection { count: count + 1 });
            }
            WasmSection::Data(data_segments) => {
                let mut data_section = DataSection::new();

                for segment in data_segments {
                    match segment {
                        WasmDataSegment::Passive(data) => {
                            data_section.passive(data.iter().copied());
                        }
                        WasmDataSegment::Active {
                            memory_index,
                            offset,
                            data,
                            i32const_offset,
                        } => {
                            if let Some(i32_offset) = i32const_offset {
                                let start = i32_offset;
                                let end = i32_offset + (data.len() as i32);

                                // Patch the data if it contains the INDEX_BASE or INDEX_LEN addresses
                                if index_base_global_address >= &start
                                    && index_base_global_address < &end
                                {
                                    assert!(
                                        index_len_global_address >= &start
                                            && index_len_global_address < &end,
                                        "INDEX_LEN address not in data segment!"
                                    );

                                    let mut data = data;

                                    let base_relative_offset =
                                        (index_base_global_address - start) as usize;
                                    data[base_relative_offset..base_relative_offset + 4]
                                        .copy_from_slice(&(index_base as i32).to_le_bytes());

                                    let length_relative_offset =
                                        (index_len_global_address - start) as usize;
                                    data[length_relative_offset..length_relative_offset + 4]
                                        .copy_from_slice(&(raw_index.len() as i32).to_le_bytes());

                                    data_section.active(memory_index, &offset, data);
                                    continue;
                                }
                            }

                            data_section.active(memory_index, &offset, data);
                        }
                    }
                }

                data_section.active(
                    0,
                    &ConstExpr::i32_const(index_base as i32),
                    raw_index.iter().copied(),
                );

                encoder.section(&data_section);
            }
            WasmSection::Memory => {
                let mut new_memory_section = MemorySection::new();
                new_memory_section.memory(MemoryType {
                    minimum: new_memory_page_count,
                    maximum: None,
                    memory64: false,
                    shared: false,
                    page_size_log2: None,
                });
                encoder.section(&new_memory_section);
            }
            WasmSection::Raw { id, data } => {
                encoder.section(&wasm_encoder::RawSection { id, data: &data });
            }
        }
    }

    let wasm_bytes = encoder.finish();
    wasmparser::Validator::new()
        .validate_all(&wasm_bytes)
        .map_err(|e| e.to_string())?;
    Ok(wasm_bytes)
}

// ---------------------------------------------------------------------------
// C-ABI export
// ---------------------------------------------------------------------------

/// Build a search index from a JSON array of documents and embed it into the
/// given base WASM template (the search module), producing the final
/// `docfind_bg.wasm` module.
///
/// Inputs:
///   docs_json_ptr/docs_json_len — JSON string: `[{title, category, href,
///     body, keywords?}]`
///   tpl_ptr/tpl_len — bytes of the base (index-less) search WASM module.
/// Outputs (written on success):
///   out_ptr/out_len — allocated buffer with the embedded WASM bytes.
/// Returns 0 on success, 1 on failure.
#[no_mangle]
pub extern "C" fn docfind_build(
    docs_json_ptr: *const u8,
    docs_json_len: usize,
    tpl_ptr: *const u8,
    tpl_len: usize,
    out_ptr: *mut usize,
    out_len: *mut usize,
) -> i32 {
    let result = (|| -> Result<Vec<u8>, String> {
        let docs_json =
            std::str::from_utf8(unsafe { input_slice(docs_json_ptr, docs_json_len) })
                .map_err(|e| e.to_string())?;
        let documents: Vec<Document> =
            serde_json::from_str(docs_json).map_err(|e| e.to_string())?;
        let template = unsafe { input_slice(tpl_ptr, tpl_len) };
        if template.is_empty() {
            return Err("template wasm is empty".to_string());
        }
        let index = build_index(documents).map_err(|e| e.to_string())?;
        let raw_index = index.to_bytes().map_err(|e| e.to_string())?;
        embed_index(template, &raw_index)
    })();

    match result {
        Ok(bytes) => {
            write_output(bytes, out_ptr, out_len);
            0
        }
        Err(_) => 1,
    }
}
