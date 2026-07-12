// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
// TypeScript has no contract construct — function only; the contract is the
// Aril-specific delta the task measures (methodology §5).
function abs(n: number): number {
  return n < 0 ? -n : n;
}
console.log(abs(-5));
console.log(abs(3));
console.log(abs(0));
