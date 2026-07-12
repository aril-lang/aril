fn main() {
    let mut xs: Vec<i32> = Vec::new();
    for i in 1..=5 {
        xs.push(i * i);
    }
    for x in &xs {
        println!("{}", x);
    }
}
