// Minimal markdown for the llms.txt handbook: # / ## headings, "- " lists,
// paragraphs and `code` spans. Input is escaped first.
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
const inline = (s) => esc(s).replace(/`([^`]+)`/g, '<code class="rounded bg-zinc-100 px-1 font-mono text-[0.85em]">$1</code>');

export function renderMarkdown(text) {
  const out = [];
  let para = [];
  let list = [];
  const flush = () => {
    if (para.length) out.push(`<p class="mb-3 leading-6">${inline(para.join(" "))}</p>`);
    if (list.length) out.push(`<ul class="mb-3 list-disc space-y-1 pl-5">${list.map((l) => `<li>${inline(l)}</li>`).join("")}</ul>`);
    para = [];
    list = [];
  };
  for (const raw of text.split("\n")) {
    const line = raw.trimEnd();
    let m;
    if ((m = line.match(/^(#{1,3})\s+(.*)$/))) {
      flush();
      const cls = m[1].length === 1 ? "mb-2 text-lg font-semibold" : "mt-5 mb-2 text-sm font-semibold uppercase tracking-wide text-zinc-600";
      out.push(`<h3 class="${cls}">${inline(m[2])}</h3>`);
    } else if ((m = line.match(/^\s*[-*]\s+(.*)$/))) {
      if (para.length) flush();
      list.push(m[1]);
    } else if (line === "") {
      flush();
    } else {
      if (list.length) flush();
      para.push(line.trim());
    }
  }
  flush();
  return out.join("");
}
