// Showcase: a generic sum type, recursion, and exhaustive matching --
// exactly where Tide's type system pays off over Go.
// Target sketch — syntax is illustrative (see docs/language-spec.md).

type Tree<T> =
  | Leaf
  | Node(value: T, left: Tree<T>, right: Tree<T>)

func invert<T>(t: Tree<T>): Tree<T> {
  match t {
    Leaf          => Leaf,
    Node(v, l, r) => Node(v, invert(r), invert(l)),
  }
}
