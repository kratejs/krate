import * as React from "react"
import { Switch as SwitchPrimitive } from "radix-ui"

function Switch({ ...props }: React.ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root data-slot="switch" {...props}>
      <SwitchPrimitive.Thumb data-slot="switch-thumb" />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
