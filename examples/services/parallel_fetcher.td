// Showcase: uncolored concurrency (D7) — any function may block, and the
// Go scheduler handles it. A structured-concurrency `scope` joins spawned
// tasks at its end; the first failure cancels its siblings (mapping onto
// errgroup + context under the hood, per docs/architecture.md §4).
//
// The program fetches a list of URLs in parallel with a per-request
// timeout and prints either the page sizes or the first error.

import context
import fmt
import http
import io
import os
import time

type Page = {
  url:  string
  size: int
}

// `scope.context` is the cancellable context of the surrounding
// concurrency scope (G18). Each fetch attaches its own deadline.
func fetch(url: string, timeout: time.Duration): Result<Page, error> {
  let (ctx, cancel) = context.withTimeout(scope.context, timeout)
  defer cancel()

  let req  = try http.newRequest("GET", url, None)
  let resp = try http.do(req.withContext(ctx))
  defer resp.body.close()

  let body = try io.readAll(resp.body)
  return Ok(Page{ url: url, size: body.len() })
}

func fetchAll(urls: []string, timeout: time.Duration): Result<[]Page, error> {
  return scope<Page, error> {
    for u in urls {
      spawn { try fetch(u, timeout) }
    }
  }
}

func main() {
  let urls = [
    "https://example.com/",
    "https://example.org/",
    "https://example.net/",
  ]

  match fetchAll(urls, time.seconds(2)) {
    Ok(pages) => {
      for p in pages {
        fmt.println(p.url, "->", p.size, "bytes")
      }
    },
    Err(e) => {
      fmt.println("fetch failed:", e)
      os.exit(1)
    },
  }
}
