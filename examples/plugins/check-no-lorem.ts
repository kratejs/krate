import { defineCheckRule } from '@krate/plugin';

// Custom quality-gate rule, executed in Krate's embedded QuickJS runtime.
// Flags placeholder copy left in shipped HTML. Referenced from `checks.custom`
// in krate.config.ts.
export default defineCheckRule((page) => {
  const findings = [];
  if (/lorem ipsum/i.test(page.html)) {
    findings.push({
      rule: 'custom/no-lorem',
      message: 'Shipped HTML contains placeholder "lorem ipsum" copy.',
      severity: 'warning',
      hint: 'Replace placeholder text before publishing.',
    });
  }
  return findings;
});
