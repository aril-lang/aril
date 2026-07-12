// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
let total = 0;
for (const p of "10 20 30 40".split(" ")) total += parseInt(p, 10);
console.log(total);
