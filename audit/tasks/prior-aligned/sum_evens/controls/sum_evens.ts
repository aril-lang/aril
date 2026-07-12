// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
let total = 0;
for (let i = 1; i <= 10; i++) {
  if (i % 2 === 0) total += i;
}
console.log(total);
