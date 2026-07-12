// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
// TypeScript has no contract construct — function only; the contract is the
// Aril-specific delta the task measures (methodology §5).
function clamp(x: number, lo: number, hi: number): number {
  if (x < lo) return lo;
  if (x > hi) return hi;
  return x;
}
console.log(clamp(5, 0, 10));
console.log(clamp(-3, 0, 10));
console.log(clamp(15, 0, 10));
