import { useState } from "react";
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
import { useRemovePlugin } from "@/queries";

type Props = { name: string };

export function RemovePluginDialog({ name }: Props) {
  const remove = useRemovePlugin(name);
  const [open, setOpen] = useState(false);

  function handleConfirm() {
    remove.mutate(undefined, {
      onSuccess: () => {
        setOpen(false);
      },
    });
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="ghost" size="sm" aria-label={`Remove ${name}`}>
          <Trash2 />
          Remove
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Remove plugin</DialogTitle>
          <DialogDescription>
            Remove plugin {name}? Workflows using it will stop validating.
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
            Remove plugin
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
