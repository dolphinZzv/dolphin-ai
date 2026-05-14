"use client"

import * as React from "react";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

interface AutocompleteProps {
  items: string[];
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  emptyText?: string;
  className?: string;
}

export function Autocomplete({
  items,
  value,
  onChange,
  placeholder = "输入...",
  emptyText = "无匹配",
  className,
}: AutocompleteProps) {
  const [open, setOpen] = React.useState(false);
  const [inputValue, setInputValue] = React.useState(value);
  const inputRef = React.useRef<HTMLInputElement>(null);

  const filtered = items.filter((item) =>
    item.toLowerCase().includes(inputValue.toLowerCase())
  );

  React.useEffect(() => {
    setInputValue(value);
  }, [value]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Input
          ref={inputRef}
          value={inputValue}
          placeholder={placeholder}
          className={cn(className)}
          onChange={(e) => {
            const v = e.target.value;
            setInputValue(v);
            onChange(v);
            setOpen(v.length > 0);
          }}
          onFocus={() => setOpen(inputValue.length > 0)}
        />
      </PopoverTrigger>
      <PopoverContent
        className="w-[--radix-popover-trigger-width] p-0"
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        <Command>
          <CommandList>
            {filtered.length === 0 ? (
              <CommandEmpty>{emptyText}</CommandEmpty>
            ) : (
              <CommandGroup>
                {filtered.map((item) => (
                  <CommandItem
                    key={item}
                    value={item}
                    onSelect={() => {
                      setInputValue(item);
                      onChange(item);
                      setOpen(false);
                      inputRef.current?.blur();
                    }}
                  >
                    {item}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
