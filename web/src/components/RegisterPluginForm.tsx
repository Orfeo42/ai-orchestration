import { useState, type SubmitEvent } from "react";
import { Plus } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useRegisterPlugin } from "@/queries";

export function RegisterPluginForm() {
  const register = useRegisterPlugin();
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const canSubmit = name.trim() !== "" && baseUrl.trim() !== "";

  function handleSubmit(e: SubmitEvent<HTMLFormElement>) {
    e.preventDefault();
    register.mutate(
      { name: name.trim(), baseUrl: baseUrl.trim() },
      {
        onSuccess: () => {
          setName("");
          setBaseUrl("");
        },
      },
    );
  }

  return (
    <form onSubmit={handleSubmit} className="grid max-w-xl gap-3">
      <div className="grid gap-2">
        <Label htmlFor="plugin-name">Name</Label>
        <Input
          id="plugin-name"
          value={name}
          onChange={(e) => {
            setName(e.target.value);
          }}
        />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="plugin-url">Base URL</Label>
        <Input
          id="plugin-url"
          value={baseUrl}
          placeholder="http://plugin:8080"
          onChange={(e) => {
            setBaseUrl(e.target.value);
          }}
        />
      </div>
      {register.error && (
        <Alert variant="destructive">
          <AlertDescription>{register.error.message}</AlertDescription>
        </Alert>
      )}
      <div>
        <Button type="submit" disabled={!canSubmit || register.isPending}>
          <Plus />
          Register
        </Button>
      </div>
    </form>
  );
}
