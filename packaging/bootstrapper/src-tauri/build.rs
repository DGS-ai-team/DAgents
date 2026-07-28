use std::env;
use std::fs;
use std::path::PathBuf;

fn main() {
    let out_dir = PathBuf::from(env::var("OUT_DIR").expect("OUT_DIR"));
    let embed_path = out_dir.join("embedded_inno.exe");

    // 发版打包时由 package-with-inno.sh 设置 DAGENTS_INNO_PAYLOAD，把 Inno 打进便携 exe。
    // 日常 tauri:dev / 无 payload 构建写入空文件，运行时走 resources 旁路或演示模式。
    if let Ok(src) = env::var("DAGENTS_INNO_PAYLOAD") {
        let src_path = PathBuf::from(&src);
        if src_path.is_file() {
            fs::copy(&src_path, &embed_path).unwrap_or_else(|e| {
                panic!("copy DAGENTS_INNO_PAYLOAD ({src}) -> {}: {e}", embed_path.display())
            });
            println!("cargo:rerun-if-changed={src}");
            println!("cargo:warning=embedding Inno payload from {src}");
        } else {
            panic!("DAGENTS_INNO_PAYLOAD is not a file: {src}");
        }
    } else {
        fs::write(&embed_path, b"").expect("write empty embedded_inno.exe");
    }

    println!("cargo:rerun-if-env-changed=DAGENTS_INNO_PAYLOAD");
    tauri_build::build()
}
