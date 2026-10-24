import { Link } from "react-router";
import type { Process } from "@/process-types";
import { SubjectLink } from "@/components/SubjectLink";
import { Card, CardContent } from "@/components/ui/card";
import { processStatuses, processStatusLabel } from "@/lib/status";

type Props = {
  processes: Process[];
};

export function ProcessBoard({ processes }: Props) {
  return (
    <div className="grid gap-3 md:grid-cols-3 xl:grid-cols-6">
      {processStatuses.map((status) => {
        const column = processes.filter((p) => p.status === status);
        return (
          <section
            key={status}
            aria-label={`${processStatusLabel[status]} column`}
            className="flex flex-col gap-2 rounded-lg bg-muted/40 p-2"
          >
            <h2 className="px-1 text-sm font-semibold">
              {processStatusLabel[status]}{" "}
              <span className="text-muted-foreground">({column.length})</span>
            </h2>
            {column.map((p) => (
              <Card key={p.id}>
                <CardContent className="flex flex-col gap-1 p-3 text-sm">
                  <Link
                    to={`/processes/${p.id}`}
                    className="font-medium hover:underline"
                  >
                    {p.title}
                  </Link>
                  <SubjectLink subject={p.subject} />
                  <span className="text-xs text-muted-foreground">
                    {p.workflow_name}
                    {p.current_step === undefined ? "" : ` / ${p.current_step}`}
                  </span>
                </CardContent>
              </Card>
            ))}
          </section>
        );
      })}
    </div>
  );
}
