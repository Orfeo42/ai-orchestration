import { useId } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useTheme } from "@/lib/theme";

type Props = {
  source: string;
};

async function renderGraph(
  id: string,
  source: string,
  dark: boolean,
): Promise<string> {
  const { default: mermaid } = await import("mermaid");
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    theme: dark ? "dark" : "default",
  });
  const { svg } = await mermaid.render(id, source);
  return svg;
}

export function WorkflowGraph({ source }: Props) {
  const { resolvedTheme } = useTheme();
  const id = `graph-${useId().replace(/[^A-Za-z0-9]/g, "")}`;
  const dark = resolvedTheme === "dark";
  const graph = useQuery({
    queryKey: ["workflow-graph", source, dark],
    queryFn: () => renderGraph(id, source, dark),
    staleTime: Infinity,
    retry: false,
  });

  if (graph.error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          Could not render graph: {graph.error.message}
        </AlertDescription>
      </Alert>
    );
  }
  if (graph.isPending) {
    return <p className="text-sm text-muted-foreground">Rendering graph...</p>;
  }
  return (
    <div
      role="img"
      aria-label="Workflow graph"
      className="overflow-auto"
      dangerouslySetInnerHTML={{ __html: graph.data }}
    />
  );
}
