import { useState } from "react";
import { useNavigate } from "react-router";
import { Trash2 } from "lucide-react";
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
import { useDeleteWorkflow } from "@/queries";

type Props = { name: string };

export function DeleteWorkflowDialog({ name }: Props) {
  const navigate = useNavigate();
  const remove = useDeleteWorkflow(name);
  const [open, setOpen] = useState(false);

  function handleConfirm() {
    remove.mutate(undefined, {
      onSuccess: () => {
        setOpen(false);
        void navigate("/workflows");
      },
    });
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <Trash2 />
          Delete
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete workflow</DialogTitle>
          <DialogDescription>
            Delete {name} and all its versions? This cannot be undone.
          </DialogDescription>
        </DialogHeader>
        {remove.error && (
          <Alert variant="destructive">
            <AlertDescription>{remove.error.message}</AlertDescription>
          </Alert>
        )}
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={remove.isPending}
            onClick={handleConfirm}
          >
            Delete workflow
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
