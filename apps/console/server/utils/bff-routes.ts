const rules: readonly (readonly [string, RegExp])[] = [
  ['GET', /^\/(me|providers|workspaces)$/],
  ['PATCH', /^\/me\/preferences$/],
  ['POST', /^\/(auth\/(email\/(start|verify)|logout)|workspaces|invitations\/accept)$/],
  ['GET', /^\/auth\/google\/(start|callback)$/],
  [
    'GET',
    /^\/workspaces\/[a-f0-9-]{36}(\/(members|connections|connections\/[a-f0-9-]{36}|sync-runs\/[a-f0-9-]{36}|imports\/[a-f0-9-]{36}\/preview|overview|costs|costs\/[a-f0-9-]{36}|invoices|invoices\/[a-f0-9-]{36}|reconciliation-runs|reconciliation-runs\/[a-f0-9-]{36}|insights|alert-rules|exports|exports\/[a-f0-9-]{36}(\/download)?|prices|subscription))?$/,
  ],
  [
    'POST',
    /^\/workspaces\/[a-f0-9-]{36}\/(invitations|deletion-requests|connections|connections\/[a-f0-9-]{36}\/(sync|credentials)|imports|imports\/[a-f0-9-]{36}\/commit|invoices|reconciliation-runs|usage-reconciliation-runs|prices|price-comparisons|alert-rules|exports|subscription\/(checkout|portal|change|cancel))$/,
  ],
  [
    'PATCH',
    /^\/workspaces\/[a-f0-9-]{36}(\/(reconciliation-items|insights|alert-rules)\/[a-f0-9-]{36})?$/,
  ],
  ['DELETE', /^\/workspaces\/[a-f0-9-]{36}\/(members|connections|alert-rules)\/[a-f0-9-]{36}$/],
]

export function isAllowedBffRoute(method: string, path: string) {
  return rules.some(([allowedMethod, pattern]) => allowedMethod === method && pattern.test(path))
}
