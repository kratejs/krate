import './radio-group.css';
import { createScope, createScopeFlags, createCSSChoice, type CSSScope } from '@krate/runtime';

const RadioGroupCtx = createScope<string>();

export interface RadioGroupProps {
  /** Literal initial selected value. */
  defaultValue?: string;
  label?: string;
  children?: any;
}

/**
 * Radio group. Zero JS via a context-provided CSS scope; items compile to hidden
 * radios plus `:has()` CSS.
 */
export function RadioGroup(props: RadioGroupProps) {
  const value = createCSSChoice<string>('', { as: 'radiogroup', label: props.label });
  return (
    <RadioGroupCtx.Provider value={value}>
      <div class="krate-radio-group">{props.children}</div>
    </RadioGroupCtx.Provider>
  );
}

export interface RadioGroupItemProps {
  value?: string;
  children?: any;
}

/** @deprecated Use `RadioGroup.Item` instead. */
export function RadioGroupItem(props: RadioGroupItemProps) {
  const scope = RadioGroupCtx.use();
  return (
    <label class="krate-radio-item" {...scope.trigger(props.value)}>
      <span class="krate-radio-indicator" aria-hidden="true" />
      <span class="krate-radio-label">{props.children}</span>
    </label>
  );
}

export interface RadioGroup {
  Item: typeof RadioGroupItem;
}

RadioGroup.Item = RadioGroupItem;
