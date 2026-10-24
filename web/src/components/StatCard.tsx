import { Card, CardContent } from "@/components/ui/card";

type Props = {
  label: string;
  value: string;
};

export function StatCard({ label, value }: Props) {
  return (
    <Card>
      <CardContent className="px-4 py-3">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">
          {label}
        </div>
        <div className="mt-1 text-xl font-semibold tabular-nums">{value}</div>
      </CardContent>
    </Card>
  );
}
