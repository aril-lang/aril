enum Shape {
    Circle(i32),
    Rect(i32, i32),
}

fn area(s: &Shape) -> i32 {
    match s {
        Shape::Circle(r) => 3 * r * r,
        Shape::Rect(w, h) => w * h,
    }
}

fn main() {
    let shapes = [Shape::Circle(2), Shape::Rect(3, 5)];
    for s in &shapes {
        println!("{}", area(s));
    }
}
