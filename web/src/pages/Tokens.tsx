import { useEffect, useState } from "react";
import { getDailyTokens } from "@/api";
import type { DailyTokensResponse } from "@/types";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Alert, AlertDescription } from "@/components/ui/alert";

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <Card>
      <CardContent className="px-4 py-3">
        <div className="text-xs uppercase tracking-wide text-muted-foreground">
          {label}
        </div>
        <div className="mt-1 text-2xl font-semibold tabular-nums">
          {value.toLocaleString()}
        </div>
      </CardContent>
    </Card>
  );
}

export default function Tokens() {
  const [data, setData] = useState<DailyTokensResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getDailyTokens()
      .then(setData)
      .catch((e) => setError(String(e)));
  }, []);

  if (error)
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  if (!data)
    return <div className="text-sm text-muted-foreground">Loading...</div>;

  const days = Object.entries(data.daily).sort(([a], [b]) =>
    a.localeCompare(b),
  );
  const totals = days.reduce(
    (acc, [, u]) => ({
      input: acc.input + u.input,
      output: acc.output + u.output,
      cache_creation_input: acc.cache_creation_input + u.cache_creation_input,
      cache_read_input: acc.cache_read_input + u.cache_read_input,
    }),
    { input: 0, output: 0, cache_creation_input: 0, cache_read_input: 0 },
  );

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Token usage</h1>
        <p className="text-sm text-muted-foreground">
          {data.from} → {data.to}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard label="Input" value={totals.input} />
        <StatCard label="Output" value={totals.output} />
        <StatCard label="Cache read" value={totals.cache_read_input} />
        <StatCard label="Cache create" value={totals.cache_creation_input} />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Daily breakdown</CardTitle>
          <CardDescription>Per-day Claude token consumption.</CardDescription>
        </CardHeader>
        <CardContent className="px-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">Date</TableHead>
                <TableHead className="text-right">Input</TableHead>
                <TableHead className="text-right">Output</TableHead>
                <TableHead className="text-right">Cache create</TableHead>
                <TableHead className="pr-6 text-right">Cache read</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {days.map(([date, u]) => (
                <TableRow key={date}>
                  <TableCell className="pl-6 font-mono text-xs">
                    {date}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {u.input.toLocaleString()}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {u.output.toLocaleString()}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {u.cache_creation_input.toLocaleString()}
                  </TableCell>
                  <TableCell className="pr-6 text-right tabular-nums">
                    {u.cache_read_input.toLocaleString()}
                  </TableCell>
                </TableRow>
              ))}
              {days.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="h-24 text-center text-muted-foreground"
                  >
                    No usage yet.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
