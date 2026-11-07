import * as React from "react"
import { Dialog as DialogPrimitive } from "radix-ui"

function Dialog({ ...props }: React.ComponentProps<typeof DialogPrimitive.Root>) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />
}

function DialogTrigger({ ...props }: React.ComponentProps<typeof DialogPrimitive.Trigger>) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />
}

function DialogPortal({ ...props }: React.ComponentProps<typeof DialogPrimitive.Portal>) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />
}

function DialogClose({ ...props }: React.ComponentProps<typeof DialogPrimitive.Close>) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />
}

// Portal children are given a ref to track presence. Preact 10 does not pass
// `ref` to plain function components (React 19 and Preact 11 do), so these two
// forward it explicitly.
const DialogOverlay = React.forwardRef<HTMLDivElement, React.ComponentProps<typeof DialogPrimitive.Overlay>>(
  function DialogOverlay(props, ref) {
    return <DialogPrimitive.Overlay ref={ref} data-slot="dialog-overlay" style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,.4)" }} {...props} />
  },
)

const DialogContent = React.forwardRef<HTMLDivElement, React.ComponentProps<typeof DialogPrimitive.Content>>(
  function DialogContent({ children, ...props }, ref) {
    return (
      <DialogPortal data-slot="dialog-portal">
        <DialogOverlay />
        <DialogPrimitive.Content ref={ref} data-slot="dialog-content" style={{ position: "fixed", top: "20%", left: "30%", background: "#fff", padding: 16 }} {...props}>
          {children}
          <DialogPrimitive.Close data-slot="dialog-close" aria-label="Close dialog">x</DialogPrimitive.Close>
        </DialogPrimitive.Content>
      </DialogPortal>
    )
  },
)

function DialogTitle({ ...props }: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return <DialogPrimitive.Title data-slot="dialog-title" {...props} />
}

function DialogDescription({ ...props }: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return <DialogPrimitive.Description data-slot="dialog-description" {...props} />
}

export { Dialog, DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogTitle, DialogTrigger }
