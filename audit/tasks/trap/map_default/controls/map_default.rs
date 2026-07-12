use std::collections::HashMap;

fn main() {
    let text = "a b a c b a";
    let mut counts: HashMap<&str, i32> = HashMap::new();
    for w in text.split(' ') {
        *counts.entry(w).or_insert(0) += 1;
    }
    for k in ["a", "b", "c", "z"] {
        println!("{}", counts.get(k).copied().unwrap_or(0));
    }
}
