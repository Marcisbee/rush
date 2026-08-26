import { init, parse } from "es-module-lexer";

export interface HoistTransformOptions {
  runtimeImport?: string;
}

interface Replacement {
  start: number;
  end: number;
  value: string;
}

interface MockCall {
  start: number;
  end: number;
  argumentsSource: string;
}

interface HoistedDeclaration {
  start: number;
  end: number;
  source: string;
}

/**
 * Hoists top-level `vi.mock()` registrations and delays static imports until after
 * registration. The native builder calls this before esbuild; no host round trip is
 * introduced at runtime.
 */
export async function transformHoistedMocks(source: string, options: HoistTransformOptions = {}): Promise<string> {
  const mocks = findMockCalls(source);
  if (mocks.length === 0) return source;
  const hoisted = findHoistedDeclarations(source);
  await init;
  const [imports] = parse(source);
  const replacements: Replacement[] = [
    ...mocks.map((mock) => ({ start: mock.start, end: mock.end, value: "" })),
    ...hoisted.map((declaration) => ({ start: declaration.start, end: declaration.end, value: "" })),
  ];

  for (const item of imports) {
    if (item.d !== -1 || item.n === undefined) continue;
    const statement = source.slice(item.ss, item.se);
    if (statement.trimStart().startsWith("export")) {
      throw new Error("Statically hoisted vi.mock cannot be combined with a re-export in the same module");
    }
    replacements.push({ start: item.ss, end: item.se, value: rewriteImport(statement, item.n) });
  }

  const runtimeImport = options.runtimeImport ?? "rush-webtest/internal";
  const runtimeImports = `__rushRegisterMock__, __rushImport__${hoisted.length > 0 ? ", vi as __rushVi" : ""}`;
  const hoistedSource = hoisted.map((declaration) => rewriteHoistedVI(declaration.source)).join("\n");
  const header = `import { ${runtimeImports} } from ${JSON.stringify(runtimeImport)};\n${hoistedSource}${hoistedSource ? "\n" : ""}${mocks.map((mock) => `__rushRegisterMock__(${mock.argumentsSource});`).join("\n")}\n`;
  return header + applyReplacements(source, replacements);
}

function rewriteImport(statement: string, sourceId: string): string {
  const trimmed = statement.trim();
  if (/^import\s+type\b/.test(trimmed)) return "";
  const loader = `__rushImport__(${JSON.stringify(sourceId)}, () => import(${JSON.stringify(sourceId)}))`;
  const sideEffect = trimmed.match(/^import\s*["'][^"']+["']\s*;?$/s);
  if (sideEffect) return `await ${loader};`;
  const match = trimmed.match(/^import\s+([\s\S]+?)\s+from\s+["'][^"']+["']\s*(?:with\s*\{[\s\S]*\})?\s*;?$/);
  if (!match?.[1]) throw new Error(`Unsupported static import while hoisting mocks: ${trimmed}`);
  const clause = match[1].trim();
  if (clause.startsWith("*")) {
    const namespace = clause.match(/^\*\s+as\s+([\w$]+)$/)?.[1];
    if (!namespace) throw new Error(`Unsupported namespace import: ${clause}`);
    return `const ${namespace} = await ${loader};`;
  }

  const comma = findTopLevelComma(clause);
  const defaultBinding = comma === -1 && !clause.startsWith("{") ? clause : comma === -1 ? undefined : clause.slice(0, comma).trim();
  const namedClause = clause.startsWith("{") ? clause : comma === -1 ? undefined : clause.slice(comma + 1).trim();
  const bindings: string[] = [];
  if (defaultBinding) bindings.push(`default: ${defaultBinding}`);
  if (namedClause) {
    const inner = namedClause.replace(/^\{/, "").replace(/\}$/, "");
    for (const binding of inner.split(",").map((part) => part.trim()).filter(Boolean)) {
      const parts = binding.split(/\s+as\s+/);
      bindings.push(parts.length === 2 ? `${parts[0]}: ${parts[1]}` : binding);
    }
  }
  return `const { ${bindings.join(", ")} } = await ${loader};`;
}

function findTopLevelComma(value: string): number {
  let depth = 0;
  for (let index = 0; index < value.length; index += 1) {
    if (value[index] === "{") depth += 1;
    else if (value[index] === "}") depth -= 1;
    else if (value[index] === "," && depth === 0) return index;
  }
  return -1;
}

function applyReplacements(source: string, replacements: Replacement[]): string {
  let output = source;
  for (const replacement of replacements.sort((left, right) => right.start - left.start)) {
    output = output.slice(0, replacement.start) + replacement.value + output.slice(replacement.end);
  }
  return output;
}

function findMockCalls(source: string): MockCall[] {
  const calls: MockCall[] = [];
  let index = 0;
  let braceDepth = 0;
  while (index < source.length) {
    const character = source[index];
    if (character === "\"" || character === "'" || character === "`") { index = skipString(source, index, character); continue; }
    if (character === "/" && source[index + 1] === "/") { index = source.indexOf("\n", index + 2); if (index === -1) break; continue; }
    if (character === "/" && source[index + 1] === "*") { const end = source.indexOf("*/", index + 2); index = end === -1 ? source.length : end + 2; continue; }
    if (character === "{") { braceDepth += 1; index += 1; continue; }
    if (character === "}") { braceDepth -= 1; index += 1; continue; }
    if (braceDepth === 0 && source.startsWith("vi.mock", index) && !isIdentifier(source[index - 1]) && !isIdentifier(source[index + 7])) {
      let open = index + 7;
      while (/\s/.test(source[open] ?? "")) open += 1;
      if (source[open] !== "(") { index += 7; continue; }
      const close = findClosingParenthesis(source, open);
      let end = close + 1;
      while (/\s/.test(source[end] ?? "")) end += 1;
      if (source[end] === ";") end += 1;
      calls.push({ start: index, end, argumentsSource: source.slice(open + 1, close) });
      index = end;
      continue;
    }
    index += 1;
  }
  return calls;
}

function findHoistedDeclarations(source: string): HoistedDeclaration[] {
  const declarations: HoistedDeclaration[] = [];
  let index = 0;
  let braceDepth = 0;
  while (index < source.length) {
    const character = source[index];
    if (character === "\"" || character === "'" || character === "`") { index = skipString(source, index, character); continue; }
    if (character === "/" && source[index + 1] === "/") { index = source.indexOf("\n", index + 2); if (index === -1) break; continue; }
    if (character === "/" && source[index + 1] === "*") { const end = source.indexOf("*/", index + 2); index = end === -1 ? source.length : end + 2; continue; }
    if (character === "{") { braceDepth += 1; index += 1; continue; }
    if (character === "}") { braceDepth -= 1; index += 1; continue; }
    if (braceDepth === 0 && isVariableDeclarationKeyword(source, index)) {
      const newline = source.slice(index).search(/[\r\n]/);
      const lineEnd = newline === -1 ? source.length : index + newline;
      const line = source.slice(index, lineEnd);
      const relative = line.indexOf("vi.hoisted");
      if (relative === -1 || isIdentifier(line[relative - 1]) || isIdentifier(line[relative + "vi.hoisted".length])) { index += 1; continue; }
      let open = index + relative + "vi.hoisted".length;
      while (/\s/.test(source[open] ?? "")) open += 1;
      if (source[open] !== "(") { index += 1; continue; }
      const close = findClosingParenthesis(source, open);
      let end = close + 1;
      while (end < source.length && source[end] !== "\n" && source[end] !== "\r") end += 1;
      declarations.push({ start: index, end, source: source.slice(index, end).trim() });
      index = end;
      continue;
    }
    index += 1;
  }
  return declarations;
}

function isVariableDeclarationKeyword(source: string, index: number): boolean {
  return ["const", "let", "var"].some((keyword) => (
    source.startsWith(keyword, index)
    && !isIdentifier(source[index - 1])
    && !isIdentifier(source[index + keyword.length])
  ));
}

function rewriteHoistedVI(source: string): string {
  let output = "";
  let index = 0;
  while (index < source.length) {
    const character = source[index];
    if (character === "\"" || character === "'" || character === "`") {
      const end = skipString(source, index, character);
      output += source.slice(index, end);
      index = end;
      continue;
    }
    if (character === "/" && source[index + 1] === "/") {
      const end = source.indexOf("\n", index + 2);
      const next = end === -1 ? source.length : end;
      output += source.slice(index, next);
      index = next;
      continue;
    }
    if (character === "/" && source[index + 1] === "*") {
      const end = source.indexOf("*/", index + 2);
      const next = end === -1 ? source.length : end + 2;
      output += source.slice(index, next);
      index = next;
      continue;
    }
    if (source.startsWith("vi", index) && !isIdentifier(source[index - 1]) && !isIdentifier(source[index + 2])) {
      output += "__rushVi";
      index += 2;
      continue;
    }
    output += character;
    index += 1;
  }
  return output;
}

function findClosingParenthesis(source: string, open: number): number {
  let depth = 1;
  for (let index = open + 1; index < source.length; index += 1) {
    const character = source[index];
    if (character === "\"" || character === "'" || character === "`") { index = skipString(source, index, character) - 1; continue; }
    if (character === "/" && source[index + 1] === "/") { const end = source.indexOf("\n", index + 2); index = end === -1 ? source.length : end; continue; }
    if (character === "/" && source[index + 1] === "*") { const end = source.indexOf("*/", index + 2); index = end === -1 ? source.length : end + 1; continue; }
    if (character === "(") depth += 1;
    if (character === ")" && --depth === 0) return index;
  }
  throw new Error("Unterminated vi.mock() call");
}

function skipString(source: string, start: number, quote: string): number {
  for (let index = start + 1; index < source.length; index += 1) {
    if (source[index] === "\\") { index += 1; continue; }
    if (source[index] === quote) return index + 1;
  }
  return source.length;
}

function isIdentifier(character: string | undefined): boolean {
  return character !== undefined && /[\w$]/.test(character);
}
