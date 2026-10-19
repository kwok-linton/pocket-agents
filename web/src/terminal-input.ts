// Focus movement cannot submit a choice — including a run like "↓↓" sent to
// reach a menu option (same rule as the backend's terminalNavigation). An
// unknown prior delivery must not block inspecting options, only submitting.
const navigation = /^(?:\x1b\[[ABCDZ]|\t)+$/;
export function needsConfirmation(bytes: string, unresolved: boolean) {
  return unresolved && !navigation.test(bytes);
}
