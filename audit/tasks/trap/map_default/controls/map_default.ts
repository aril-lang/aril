// Reference-language control (TypeScript). Statement only — not machine-validated
// in this environment (no Node/tsc). Same I/O contract as the task.
const counts = new Map<string, number>();
for (const w of "a b a c b a".split(" ")) counts.set(w, (counts.get(w) ?? 0) + 1);
for (const k of ["a", "b", "c", "z"]) console.log(counts.get(k) ?? 0);
