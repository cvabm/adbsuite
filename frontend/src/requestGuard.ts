// Only the most recent request in a mounted view may publish its result.
export function createRequestGuard() {
  let generation = 0;
  let active = true;
  return {
    begin: () => ++generation,
    current: (ticket: number) => active && ticket === generation,
    active: () => active,
    activate: () => { active = true; ++generation; },
    dispose: () => { active = false; ++generation; },
  };
}
