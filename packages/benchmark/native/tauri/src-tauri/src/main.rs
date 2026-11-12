// Tauri side of the benchmark: two commands the page calls with invoke().
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

#[tauri::command]
fn ping(n: i64) -> String {
    format!("pong {} {}", std::env::consts::OS, n + 1)
}

// Writes the first-paint record where the harness asked for it.
#[tauri::command]
fn report(record: String) -> Result<(), String> {
    if let Ok(path) = std::env::var("BENCH_REPORT") {
        std::fs::write(path, format!("{record}\n")).map_err(|e| e.to_string())?;
    }
    Ok(())
}

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![ping, report])
        .run(tauri::generate_context!())
        .expect("error while running the benchmark app");
}
