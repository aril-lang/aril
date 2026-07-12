// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
type Shape = { kind: "circle"; r: number } | { kind: "rect"; w: number; h: number };

function area(s: Shape): number {
  switch (s.kind) {
    case "circle": return 3 * s.r * s.r;
    case "rect": return s.w * s.h;
  }
}

const shapes: Shape[] = [{ kind: "circle", r: 2 }, { kind: "rect", w: 3, h: 5 }];
for (const s of shapes) console.log(area(s));
