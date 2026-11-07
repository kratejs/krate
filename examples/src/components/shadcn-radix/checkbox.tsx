import * as React from "react"
import { Checkbox as CheckboxPrimitive } from "radix-ui"

function Checkbox({ ...props }: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root data-slot="checkbox" {...props}>
      <CheckboxPrimitive.Indicator data-slot="checkbox-indicator">v</CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

export { Checkbox }
