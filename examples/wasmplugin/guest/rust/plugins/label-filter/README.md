# Rust Label Filter

Example wasm filter plugin written in Rust. Keeps only endpoints with
`gpu-type: a100`.

## Build

```bash
rustup target add wasm32-wasip1
cargo build --target wasm32-wasip1 --release
```

The output is at `target/wasm32-wasip1/release/label_filter.wasm`.
