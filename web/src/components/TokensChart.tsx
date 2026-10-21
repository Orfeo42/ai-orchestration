import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { TokenPoint } from "@/process-types";

type Props = {
  points: TokenPoint[];
};

const INITIAL_DIMENSION = { width: 640, height: 280 };

export function TokensChart({ points }: Props) {
  const data = points.map((p) => ({
    bucket: p.bucket,
    input: p.usage.input,
    output: p.usage.output,
  }));
  return (
    <div role="img" aria-label="Tokens chart" className="h-70 w-full">
      <ResponsiveContainer
        width="100%"
        height="100%"
        initialDimension={INITIAL_DIMENSION}
      >
        <BarChart data={data}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
          <XAxis
            dataKey="bucket"
            stroke="var(--color-muted-foreground)"
            tick={{ fill: "var(--color-muted-foreground)", fontSize: 12 }}
          />
          <YAxis
            stroke="var(--color-muted-foreground)"
            tick={{ fill: "var(--color-muted-foreground)", fontSize: 12 }}
          />
          <Tooltip
            contentStyle={{
              background: "var(--color-popover)",
              color: "var(--color-popover-foreground)",
              border: "1px solid var(--color-border)",
            }}
          />
          <Legend />
          <Bar
            dataKey="input"
            name="Input"
            stackId="tokens"
            fill="var(--color-chart-1)"
          />
          <Bar
            dataKey="output"
            name="Output"
            stackId="tokens"
            fill="var(--color-chart-2)"
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
