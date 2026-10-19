// Recognise a native selection menu at the bottom of the terminal screen, as
// drawn by Claude Code and similar TUIs:
//
//   ❯ 1. Yes
//     2. Yes, and don't ask again
//     3. No, and tell Claude what to do differently (esc)
//
// Checkbox menus add "[ ]" / "[✓]". Choosing an option on the phone only moves
// the native highlight (arrow keys); confirming is a separate Enter.
export interface MenuOption {
  /** null is the native, unnumbered Submit row in a checkbox question. */
  number: number | null;
  label: string;
  checked?: boolean;
}
export interface TerminalMenu {
  options: MenuOption[];
  /** Position in options of the natively highlighted row, -1 if unknown. */
  selected: number;
}

const OPTION = /^\s*([❯›>▶])?\s*(\d{1,2})[.)]\s+(?:\[([ xX✓✔])\]\s+)?(.+?)\s*$/;
const SUBMIT = /^\s*([❯›>▶])?\s+Submit\s*$/;
const SEPARATOR = /^\s*[─━—\-]{3,}\s*$/;

export function terminalMenu(screen: string | undefined): TerminalMenu | null {
  if (!screen) return null;
  const lines = screen.split("\n");
  // Walk up from the bottom to the last run of numbered options, skipping the
  // hint/footer lines and indented descriptions that menus draw around them.
  let end = lines.length - 1;
  while (end >= 0 && !OPTION.test(lines[end])) end--;
  if (end < 0 || lines.length - 1 - end > 8) return null;
  const options: MenuOption[] = [];
  const highlighted: boolean[] = [];
  let nextNumber: number | undefined;
  for (let i = end; i >= 0; i--) {
    const match = OPTION.exec(lines[i]);
    if (!match) {
      const submit = SUBMIT.exec(lines[i]);
      if (submit && options.length) {
        options.unshift({ number: null, label: "Submit" });
        highlighted.unshift(!!submit[1]);
        continue;
      }
      // A wrapped label or description line inside the menu.
      if (options.length && (!lines[i].trim() || SEPARATOR.test(lines[i]) || /^\s{2,}\S/.test(lines[i]))) continue;
      break;
    }
    const number = Number(match[2]);
    if (nextNumber !== undefined && number !== nextNumber - 1) break;
    options.unshift({ number, label: match[4], ...(match[3] !== undefined ? { checked: match[3] !== " " } : {}) });
    highlighted.unshift(!!match[1]);
    nextNumber = number;
    if (number === 1) break;
  }
  if (options.length < 2 || options[0].number !== 1) return null;
  // An unnumbered submit control is only meaningful inside a checkbox menu.
  if (options.some((o) => o.number === null) && !options.some((o) => o.checked !== undefined)) return null;
  if (highlighted.filter(Boolean).length > 1) return null;
  return { options, selected: highlighted.indexOf(true) };
}

/** Arrow keys that move the native highlight from the current row to target. */
export function moveKeys(menu: TerminalMenu, target: number) {
  if (menu.selected < 0 || target === menu.selected) return "";
  const step = target > menu.selected ? "\x1b[B" : "\x1b[A";
  return step.repeat(Math.abs(target - menu.selected));
}
