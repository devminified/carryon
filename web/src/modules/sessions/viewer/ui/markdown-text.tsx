import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

// No rehype-raw plugin — react-markdown never renders raw HTML found in
// the source text without it, which is exactly the point (CLAUDE.md:
// "a transcript contains text Claude read from the open web" — sanitize
// Markdown, no raw HTML, same rule as the viewer's own anti-pattern
// entry).
export function MarkdownText({ text }: { text: string }) {
  return (
    <div className="prose prose-sm dark:prose-invert max-w-none">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
    </div>
  );
}
