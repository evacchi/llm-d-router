use llm_d_wasm_guest::{register_filter, log, Endpoint, Request};

#[no_mangle]
extern "C" fn _initialize() {
    register_filter(filter_fn);
}

fn filter_fn(_req: &Request, endpoints: &[Endpoint]) -> Vec<String> {
    log("rust-label-filter: keeping gpu-type=a100 endpoints");
    endpoints
        .iter()
        .filter(|ep| ep.labels.get("gpu-type").map(|v| v.as_str()) == Some("a100"))
        .map(|ep| ep.id.clone())
        .collect()
}
