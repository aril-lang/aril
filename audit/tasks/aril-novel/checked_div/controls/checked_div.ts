// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
function safeDiv(a: number, b: number): number | Error {
  if (b === 0) return new Error("division by zero");
  return Math.trunc(a / b);
}

const cases: [number, number][] = [[10, 2], [7, 0], [9, 3]];
for (const [a, b] of cases) {
  const r = safeDiv(a, b);
  if (r instanceof Error) console.log("err", r.message);
  else console.log("ok", r);
}
