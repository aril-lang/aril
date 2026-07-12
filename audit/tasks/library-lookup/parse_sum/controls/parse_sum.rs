fn main() {
    let data = "10 20 30 40";
    let mut total = 0;
    for part in data.split(' ') {
        total += part.parse::<i32>().unwrap_or(0);
    }
    println!("{}", total);
}
