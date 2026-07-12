// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
const xs = [3, 1, 4, 1, 5, 9, 2, 6];
xs.sort((a, b) => b - a);
for (const x of xs) console.log(x);
