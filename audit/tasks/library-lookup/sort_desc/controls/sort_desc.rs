fn main() {
    let mut xs = vec![3, 1, 4, 1, 5, 9, 2, 6];
    xs.sort_by(|a, b| b.cmp(a));
    for x in &xs {
        println!("{}", x);
    }
}
