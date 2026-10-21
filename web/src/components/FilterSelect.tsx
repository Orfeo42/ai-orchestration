import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const ALL = "__all__";

export interface SelectOption {
  value: string;
  label: string;
}

type Props = {
  label: string;
  value: string;
  options: readonly SelectOption[];
  onChange: (value: string) => void;
  allLabel?: string;
  className?: string;
  id?: string;
};

export function FilterSelect({
  label,
  value,
  options,
  onChange,
  allLabel,
  className,
  id,
}: Props) {
  const current = value === "" && allLabel !== undefined ? ALL : value;

  function handleChange(next: string) {
    onChange(next === ALL ? "" : next);
  }

  return (
    <Select value={current} onValueChange={handleChange}>
      <SelectTrigger id={id} aria-label={label} className={className}>
        <SelectValue placeholder={label} />
      </SelectTrigger>
      <SelectContent>
        {allLabel !== undefined && (
          <SelectItem value={ALL}>{allLabel}</SelectItem>
        )}
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
