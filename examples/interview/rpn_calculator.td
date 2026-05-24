// Showcase: Result / try / match. Sum-typed operators, errors as values.
// Target sketch — syntax is illustrative (see docs/language-spec.md).
import strings
import strconv

type Op = | Add | Sub | Mul | Div

func apply(op: Op, a: float64, b: float64): Result<float64, error> {
  match op {
    Add => Ok(a + b),
    Sub => Ok(a - b),
    Mul => Ok(a * b),
    Div => if b == 0.0 {
             Err(error("division by zero"))
           } else {
             Ok(a / b)
           },
  }
}

func evalRPN(expr: string): Result<float64, error> {
  let stack = makeStack<float64>()
  for token in strings.fields(expr) {
    match parseOp(token) {
      Some(op) => {
        let b = try stack.pop()
        let a = try stack.pop()
        stack.push(try apply(op, a, b))
      },
      None => stack.push(try strconv.parseFloat(token)),
    }
  }
  return stack.pop()
}
