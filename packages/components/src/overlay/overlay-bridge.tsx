export interface OverlayBridgeProps {
  /** Controlled open state. Undefined leaves the overlay uncontrolled. */
  open?: boolean;
  /** Called with the new state whenever the overlay opens or closes. */
  onOpenChange?: (open: boolean) => void;
}

/**
 * Hydrated glue rendered inside an overlay only when it is given `open` or
 * `onOpenChange`, so overlays that use neither keep shipping no page JS. The
 * keyboard enhancer applies `data-kopen` to the overlay and re-dispatches
 * open/close changes to this element as a `toggle` event.
 */
export function OverlayBridge(props: OverlayBridgeProps) {
  return (
    <span
      class="krate-overlay-bridge"
      hidden
      data-kzo="true"
      data-kopen={props.open === undefined ? undefined : props.open ? 'open' : 'closed'}
      {...({
        onToggle: (e: any) => {
          const d = (e.nativeEvent || e).detail;
          if (props.onOpenChange) props.onOpenChange(!!(d && d.open));
        },
      } as any)}
    />
  );
}
