// Rust has no first-class contract construct — this control implements the
// function only; the contract is the Aril-specific delta (methodology §5).
fn abs(n: i32) -> i32 {
    if n < 0 {
        -n
    } else {
        n
    }
}

fn main() {
    println!("{}", abs(-5));
    println!("{}", abs(3));
    println!("{}", abs(0));
}
