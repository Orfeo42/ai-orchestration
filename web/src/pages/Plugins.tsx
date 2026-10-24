import { PluginRow } from "@/components/PluginRow";
import { RegisterPluginForm } from "@/components/RegisterPluginForm";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { usePlugins } from "@/queries";

export function Plugins() {
  const plugins = usePlugins();

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Plugins</h1>
        <p className="text-sm text-muted-foreground">
          Registered plugins and the steps and triggers they provide.
        </p>
      </div>
      <RegisterPluginForm />
      {plugins.error && (
        <Alert variant="destructive">
          <AlertDescription>{plugins.error.message}</AlertDescription>
        </Alert>
      )}
      {plugins.isPending && (
        <p className="text-sm text-muted-foreground">Loading plugins...</p>
      )}
      {plugins.data && (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Base URL</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                  <TableHead className="text-right">Triggers</TableHead>
                  <TableHead>Checked</TableHead>
                  <TableHead>Error</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {plugins.data.map((p) => (
                  <PluginRow key={p.name} plugin={p} />
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
