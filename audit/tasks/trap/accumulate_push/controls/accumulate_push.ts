// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
const xs: number[] = [];
for (let i = 1; i <= 5; i++) xs.push(i * i);
for (const x of xs) console.log(x);
