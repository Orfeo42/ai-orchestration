import { useState, type SubmitEvent } from "react";
import { useNavigate } from "react-router";
import { Plus } from "lucide-react";
import { FilterSelect } from "@/components/FilterSelect";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useStartProcess, useWorkflows } from "@/queries";

export function StartProcessDialog() {
  const navigate = useNavigate();
  const workflows = useWorkflows();
  const start = useStartProcess();
  const [open, setOpen] = useState(false);
  const [workflow, setWorkflow] = useState("");
  const [subjectKey, setSubjectKey] = useState("");

  const options = (workflows.data ?? []).map((w) => ({
    value: w.name,
    label: w.name,
  }));
  const canSubmit = workflow !== "" && subjectKey.trim() !== "";

  function handleSubmit(e: SubmitEvent<HTMLFormElement>) {
    e.preventDefault();
    start.mutate(
      { workflow, subjectKey: subjectKey.trim() },
      {
        onSuccess: (created) => {
          setOpen(false);
          setSubjectKey("");
          void navigate(`/processes/${created.id}`);
        },
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus className="h-3.5 w-3.5" />
          Start process
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={handleSubmit} className="grid gap-4">
          <DialogHeader>
            <DialogTitle>Start process</DialogTitle>
            <DialogDescription>
              Run a workflow against an issue.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="start-workflow">Workflow</Label>
            <FilterSelect
              id="start-workflow"
              label="Workflow to run"
              value={workflow}
              options={options}
              onChange={setWorkflow}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="subject-key">Subject key</Label>
            <Input
              id="subject-key"
              value={subjectKey}
              onChange={(e) => {
                setSubjectKey(e.target.value);
              }}
              placeholder="owner/repo#n"
            />
          </div>
          {start.error && (
            <Alert variant="destructive">
              <AlertDescription>{start.error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button type="submit" disabled={!canSubmit || start.isPending}>
              Start
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
