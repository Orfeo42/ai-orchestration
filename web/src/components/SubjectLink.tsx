import type { Subject } from "@/process-types";

type Props = {
  subject: Subject;
};

export function SubjectLink({ subject }: Props) {
  if (subject.url === undefined || subject.url === "") {
    return <span className="font-mono text-xs">{subject.key}</span>;
  }
  return (
    <a
      href={subject.url}
      target="_blank"
      rel="noreferrer"
      className="font-mono text-xs text-sky-600 hover:underline dark:text-sky-400"
    >
      {subject.key}
    </a>
  );
}
