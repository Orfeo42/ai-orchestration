import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

interface Props {
  text: string;
  className?: string;
}

export default function Markdown({ text, className }: Props) {
  return (
    <div
      className={cn(
        "prose-sm max-w-none break-words text-sm leading-6",
        "[&_p]:my-1 [&_p:first-child]:mt-0 [&_p:last-child]:mb-0",
        "[&_ul]:my-1 [&_ul]:list-disc [&_ul]:pl-5",
        "[&_ol]:my-1 [&_ol]:list-decimal [&_ol]:pl-5",
        "[&_li]:my-0.5",
        "[&_h1]:mt-2 [&_h1]:text-base [&_h1]:font-semibold",
        "[&_h2]:mt-2 [&_h2]:text-sm [&_h2]:font-semibold",
        "[&_h3]:mt-2 [&_h3]:text-sm [&_h3]:font-semibold",
        "[&_strong]:font-semibold",
        "[&_em]:italic",
        "[&_a]:text-sky-500 [&_a:hover]:underline",
        "[&_blockquote]:my-1 [&_blockquote]:border-l-2 [&_blockquote]:border-muted-foreground/30 [&_blockquote]:pl-3 [&_blockquote]:text-muted-foreground",
        "[&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-xs",
        "[&_pre]:my-2 [&_pre]:overflow-auto [&_pre]:rounded-md [&_pre]:bg-muted [&_pre]:p-3 [&_pre]:text-xs",
        "[&_pre_code]:bg-transparent [&_pre_code]:p-0",
        "[&_table]:my-2 [&_table]:w-full [&_table]:border-collapse",
        "[&_th]:border [&_th]:border-border [&_th]:bg-muted [&_th]:px-2 [&_th]:py-1 [&_th]:text-left",
        "[&_td]:border [&_td]:border-border [&_td]:px-2 [&_td]:py-1",
        "[&_hr]:my-2 [&_hr]:border-border",
        className,
      )}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
    </div>
  );
}
