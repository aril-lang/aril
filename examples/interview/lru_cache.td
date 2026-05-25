// Showcase: a generic class with methods, combining a map (for O(1) lookup)
// with a doubly-linked list (for O(1) eviction-order maintenance).
// Demonstrates static factory methods, instance methods, mutation through
// `this.field`, and that classes are reference types (G16) — the list
// nodes referenced from the map are the same objects mutated by `touch`.

import fmt

class LRUNode<K, V> {
  let key:   K
  var value: V
  var prev:  Option<LRUNode<K, V>>
  var next:  Option<LRUNode<K, V>>
}

class LRU<K, V> {
  let capacity: int

  var size:  int
  var head:  Option<LRUNode<K, V>>      // most recently used
  var tail:  Option<LRUNode<K, V>>      // least recently used (eviction target)
  var index: Map<K, LRUNode<K, V>>

  static new(capacity: int): LRU<K, V> {
    return LRU<K, V>{
      capacity: capacity,
      size:     0,
      head:     None,
      tail:     None,
      index:    Map<K, LRUNode<K, V>>{},
    }
  }

  // Returns the cached value for `k`, marking it most-recently-used.
  get(k: K): Option<V> {
    match this.index.get(k) {
      None    => None,
      Some(n) => {
        this.touch(n)
        Some(n.value)
      },
    }
  }

  // Inserts or overwrites. Evicts the least-recently-used entry if full.
  put(k: K, v: V) {
    match this.index.get(k) {
      Some(n) => {
        n.value = v
        this.touch(n)
        return
      },
      None => {},
    }
    let n = LRUNode<K, V>{
      key:   k,
      value: v,
      prev:  None,
      next:  this.head,
    }
    match this.head {
      Some(h) => h.prev = Some(n),
      None    => this.tail = Some(n),
    }
    this.head = Some(n)
    this.index.set(k, n)
    this.size = this.size + 1
    if this.size > this.capacity {
      this.evictTail()
    }
  }

  len(): int { return this.size }

  // ----- internals -----

  touch(n: LRUNode<K, V>) {
    match this.head {
      Some(h) => if refEq(h, n) { return },
      None    => return,
    }
    this.detach(n)
    n.next = this.head
    n.prev = None
    match this.head {
      Some(h) => h.prev = Some(n),
      None    => {},
    }
    this.head = Some(n)
  }

  detach(n: LRUNode<K, V>) {
    match n.prev {
      Some(p) => p.next = n.next,
      None    => this.head = n.next,
    }
    match n.next {
      Some(nx) => nx.prev = n.prev,
      None     => this.tail = n.prev,
    }
    n.prev = None
    n.next = None
  }

  evictTail() {
    match this.tail {
      None    => return,
      Some(t) => {
        this.detach(t)
        this.index.delete(t.key)
        this.size = this.size - 1
      },
    }
  }
}

func main() {
  let cache = LRU<string, int>.new(2)
  cache.put("a", 1)
  cache.put("b", 2)
  fmt.println("get a:", cache.get("a"))       // -> Some(1); a becomes MRU
  cache.put("c", 3)                            // evicts b
  fmt.println("get b:", cache.get("b"))       // -> None
  fmt.println("get c:", cache.get("c"))       // -> Some(3)
  fmt.println("len:",   cache.len())          // -> 2
}
