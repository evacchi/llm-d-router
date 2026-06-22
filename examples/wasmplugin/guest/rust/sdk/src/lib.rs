use serde::{Deserialize, Serialize};
use std::collections::HashMap;

// ── ABI types ───────────────────────────────────────────────────────

#[derive(Deserialize, Debug)]
pub struct Request {
    pub request_id: String,
    pub target_model: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    #[serde(default)]
    pub request_size_bytes: usize,
}

#[derive(Deserialize, Debug)]
pub struct Endpoint {
    pub id: String,
    pub address: String,
    pub port: String,
    #[serde(default)]
    pub labels: HashMap<String, String>,
    #[serde(default)]
    pub metrics: Metrics,
}

#[derive(Deserialize, Debug, Default)]
pub struct Metrics {
    #[serde(default)]
    pub active_models: HashMap<String, i32>,
    #[serde(default)]
    pub waiting_models: HashMap<String, i32>,
    #[serde(default)]
    pub running_requests_size: i32,
    #[serde(default)]
    pub waiting_queue_size: i32,
    #[serde(default)]
    pub kv_cache_usage_percent: f64,
}

// ── Internal ABI plumbing ───────────────────────────────────────────

#[derive(Deserialize)]
struct FilterInput {
    request: Request,
    endpoints: Vec<Endpoint>,
}

#[derive(Serialize)]
struct FilterOutput {
    endpoint_ids: Vec<String>,
}

#[derive(Deserialize)]
struct ScorerInput {
    request: Request,
    endpoints: Vec<Endpoint>,
}

#[derive(Serialize)]
struct ScorerOutput {
    scores: HashMap<String, f64>,
}

static mut BUF: [u8; 1 << 20] = [0; 1 << 20];
static mut OFFSET: usize = 0;

#[no_mangle]
pub extern "C" fn alloc(size: i32) -> *mut u8 {
    unsafe {
        let off = OFFSET;
        if off + size as usize > BUF.len() {
            return std::ptr::null_mut();
        }
        OFFSET += size as usize;
        BUF.as_mut_ptr().add(off)
    }
}

fn write_output(data: &[u8]) -> i64 {
    let ptr = alloc(data.len() as i32);
    if ptr.is_null() {
        return 0;
    }
    unsafe {
        std::ptr::copy_nonoverlapping(data.as_ptr(), ptr, data.len());
    }
    ((ptr as u64) << 32) | (data.len() as u64)
}

extern "C" {
    fn log_message(ptr: *const u8, len: i32);
}

pub fn log(msg: &str) {
    unsafe { log_message(msg.as_ptr(), msg.len() as i32) }
}

// ── Public API ──────────────────────────────────────────────────────

pub type FilterFn = fn(request: &Request, endpoints: &[Endpoint]) -> Vec<String>;
pub type ScorerFn = fn(request: &Request, endpoints: &[Endpoint]) -> HashMap<String, f64>;

static mut FILTER_FN: Option<FilterFn> = None;
static mut SCORER_FN: Option<ScorerFn> = None;

pub fn register_filter(f: FilterFn) {
    unsafe { FILTER_FN = Some(f) }
}

pub fn register_scorer(f: ScorerFn) {
    unsafe { SCORER_FN = Some(f) }
}

#[no_mangle]
pub extern "C" fn filter(ptr: *const u8, len: i32) -> i64 {
    unsafe { OFFSET = 0 };
    let bytes = unsafe { std::slice::from_raw_parts(ptr, len as usize) };
    let input: FilterInput = match serde_json::from_slice(bytes) {
        Ok(v) => v,
        Err(_) => return 0,
    };
    let f = match unsafe { FILTER_FN } {
        Some(f) => f,
        None => return 0,
    };
    let ids = f(&input.request, &input.endpoints);
    let out = serde_json::to_vec(&FilterOutput { endpoint_ids: ids }).unwrap_or_default();
    write_output(&out)
}

#[no_mangle]
pub extern "C" fn score(ptr: *const u8, len: i32) -> i64 {
    unsafe { OFFSET = 0 };
    let bytes = unsafe { std::slice::from_raw_parts(ptr, len as usize) };
    let input: ScorerInput = match serde_json::from_slice(bytes) {
        Ok(v) => v,
        Err(_) => return 0,
    };
    let f = match unsafe { SCORER_FN } {
        Some(f) => f,
        None => return 0,
    };
    let scores = f(&input.request, &input.endpoints);
    let out = serde_json::to_vec(&ScorerOutput { scores }).unwrap_or_default();
    write_output(&out)
}
