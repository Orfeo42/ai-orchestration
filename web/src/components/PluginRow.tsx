import { useState } from "react";
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import { RemovePluginDialog } from "@/components/RemovePluginDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { formatDateTime } from "@/lib/format";
import { useRefreshPlugin } from "@/queries";
import type { PluginRecord, PluginStatus } from "@/types";

const statusVariant: Record<
  PluginStatus,
  "success" | "warning" | "destructive" | "secondary"
> = {
  ok: "success",
  degraded: "warning",
  down: "destructive",
  unreachable: "secondary",
};

type Props = { plugin: PluginRecord };

export function PluginRow({ plugin }: Props) {
  const [expanded, setExpanded] = useState(false);
  const refresh = useRefreshPlugin(plugin.name);
  const triggers = plugin.manifest.triggers ?? [];
  const errorText = refresh.error?.message ?? plugin.error ?? "";

  return (
    <>
      <TableRow>
        <TableCell className="font-medium">
          <Button
            variant="ghost"
            size="icon"
            aria-label={`Details for ${plugin.name}`}
            aria-expanded={expanded}
            onClick={() => {
              setExpanded((v) => !v);
            }}
          >
            {expanded ? <ChevronDown /> : <ChevronRight />}
          </Button>
          {plugin.name}
        </TableCell>
        <TableCell className="font-mono text-xs">{plugin.base_url}</TableCell>
        <TableCell>
          <Badge variant={statusVariant[plugin.status]}>{plugin.status}</Badge>
        </TableCell>
        <TableCell className="text-right tabular-nums">
          {plugin.manifest.actions.length}
        </TableCell>
        <TableCell className="text-right tabular-nums">
          {triggers.length}
        </TableCell>
        <TableCell>{formatDateTime(plugin.checked_at)}</TableCell>
        <TableCell className="text-xs text-destructive">{errorText}</TableCell>
        <TableCell className="whitespace-nowrap text-right">
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Refresh ${plugin.name}`}
            disabled={refresh.isPending}
            onClick={() => {
              refresh.mutate();
            }}
          >
            <RefreshCw />
            Refresh
          </Button>
          <RemovePluginDialog name={plugin.name} />
        </TableCell>
      </TableRow>
      {expanded && (
        <TableRow>
          <TableCell colSpan={8} className="bg-muted/30">
            <div className="grid gap-3 p-2">
              <div>
                <h3 className="text-sm font-medium">Actions</h3>
                <ul className="mt-1 grid gap-2">
                  {plugin.manifest.actions.map((a) => (
                    <li key={a.name} className="text-sm">
                      <span className="font-mono">{a.name}</span>{" "}
                      <Badge variant="outline">{a.mode}</Badge>{" "}
                      {a.outcomes.map((o) => (
                        <Badge key={o} variant="secondary" className="mr-1">
                          {o}
                        </Badge>
                      ))}
                      <p className="text-xs text-muted-foreground">
                        {a.description}
                      </p>
                    </li>
                  ))}
                </ul>
              </div>
              {triggers.length > 0 && (
                <div>
                  <h3 className="text-sm font-medium">Triggers</h3>
                  <ul className="mt-1 grid gap-2">
                    {triggers.map((t) => (
                      <li key={t.name} className="text-sm">
                        <span className="font-mono">{t.name}</span>
                        <p className="text-xs text-muted-foreground">
                          {t.description}
                        </p>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  );
}
