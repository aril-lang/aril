// Rust has no first-class contract construct — this control implements the
// function only; the contract is the Aril-specific delta (methodology §5).
fn clamp(x: i32, lo: i32, hi: i32) -> i32 {
    if x < lo {
        lo
    } else if x > hi {
        hi
    } else {
        x
    }
}

fn main() {
    println!("{}", clamp(5, 0, 10));
    println!("{}", clamp(-3, 0, 10));
    println!("{}", clamp(15, 0, 10));
}
