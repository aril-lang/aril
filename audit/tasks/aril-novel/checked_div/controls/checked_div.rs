fn safe_div(a: i32, b: i32) -> Result<i32, String> {
    if b == 0 {
        return Err(String::from("division by zero"));
    }
    Ok(a / b)
}

fn main() {
    let cases = [(10, 2), (7, 0), (9, 3)];
    for (a, b) in cases {
        match safe_div(a, b) {
            Ok(q) => println!("ok {}", q),
            Err(e) => println!("err {}", e),
        }
    }
}
