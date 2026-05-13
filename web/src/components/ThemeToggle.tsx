import { Monitor, Moon, Sun } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useTheme, type Theme } from "@/lib/theme";

export function ThemeToggle({ className }: { className?: string }) {
  const { theme, setTheme } = useTheme();

  return (
    <Select value={theme} onValueChange={(v) => setTheme(v as Theme)}>
      <SelectTrigger className={className} aria-label="Theme">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="dark">
          <span className="flex items-center gap-2">
            <Moon className="h-4 w-4" />
            Dark
          </span>
        </SelectItem>
        <SelectItem value="light">
          <span className="flex items-center gap-2">
            <Sun className="h-4 w-4" />
            Light
          </span>
        </SelectItem>
        <SelectItem value="system">
          <span className="flex items-center gap-2">
            <Monitor className="h-4 w-4" />
            System
          </span>
        </SelectItem>
      </SelectContent>
    </Select>
  );
}
