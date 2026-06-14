#!/usr/bin/env node
// JSONC-aware helper for VS Code settings/keybindings/mcp files.
// Extracts and applies a managed subset of top-level keys while preserving
// comments, trailing commas, and formatting in the user's live file AND on the
// managed keys themselves. Uses jsonc-parser (the library VS Code ships).
//
// Usage:
//   jsonc.mjs extract <liveFile> <keyRegex>
//       -> prints a JSONC object of the managed top-level keys, INCLUDING any
//          comment lines that immediately precede each managed key.
//          Missing file or no managed keys -> "{}".
//
//   jsonc.mjs apply <liveFile> <profileFileOrNONE> <keyRegex>
//       -> rewrites <liveFile> in place: strips all managed keys, then
//          (if a profile file is given) re-inserts each managed key WITH its
//          stored comments. Comments/formatting on untouched (non-managed)
//          keys are preserved. profileFileOrNONE == "NONE" means clean.

import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { parseTree, applyEdits, format } from 'jsonc-parser';

const FORMAT = { insertSpaces: true, tabSize: 2 };
const PARSE = { allowTrailingComma: true };

function die(msg) {
  process.stderr.write(`jsonc.mjs: ${msg}\n`);
  process.exit(1);
}

function rootObject(text) {
  const root = parseTree(text, [], PARSE);
  return root && root.type === 'object' ? root : null;
}

function properties(root) {
  return (root?.children ?? []).filter(
    (p) => p.type === 'property' && p.children?.length,
  );
}

// Capture the run of comment-only lines that immediately precede a property,
// looking only in the gap between the previous sibling (or the opening brace)
// and this property. Returns the comment text (no trailing newline) or "".
function leadingComments(text, gapStart, propStart) {
  const gap = text.slice(gapStart, propStart);
  const lines = gap.split('\n');
  // The final element is the indentation directly before the key; drop it.
  lines.pop();
  const collected = [];
  for (let i = lines.length - 1; i >= 0; i--) {
    const t = lines[i].trim();
    if (t.startsWith('//') || t.startsWith('/*') || t.startsWith('*') || t.endsWith('*/')) {
      collected.unshift(t);
    } else {
      break; // stop at blank line, the comma, or any non-comment content
    }
  }
  return collected.join('\n');
}

// Ordered [{ key, text }] for managed properties, where text is the property's
// source (`"key": value`) prefixed by its captured leading comments.
function managedSpans(text, re) {
  const root = rootObject(text);
  if (!root) return [];
  const props = properties(root);
  const spans = [];
  for (let i = 0; i < props.length; i++) {
    const prop = props[i];
    const key = prop.children[0].value;
    if (!re.test(key)) continue;
    const gapStart = i === 0 ? root.offset + 1 : props[i - 1].offset + props[i - 1].length;
    const comments = leadingComments(text, gapStart, prop.offset);
    const body = text.slice(prop.offset, prop.offset + prop.length);
    spans.push({ key, text: (comments ? comments + '\n' : '') + body });
  }
  return spans;
}

const [, , cmd, ...args] = process.argv;

if (cmd === 'extract') {
  const [file, keyRegex] = args;
  if (!file || !keyRegex) die('extract <liveFile> <keyRegex>');
  if (!existsSync(file)) {
    process.stdout.write('{}\n');
    process.exit(0);
  }
  const re = new RegExp(keyRegex);
  const text = readFileSync(file, 'utf8');
  const spans = managedSpans(text, re);
  if (spans.length === 0) {
    process.stdout.write('{}\n');
    process.exit(0);
  }
  let out = '{\n' + spans.map((s) => s.text).join(',\n') + '\n}\n';
  const edits = format(out, undefined, FORMAT);
  out = applyEdits(out, edits);
  process.stdout.write(out.replace(/\n*$/, '') + '\n');
  process.exit(0);
}

// True if a trimmed line is purely a comment (no JSON content).
function isCommentLine(s) {
  const t = s.trim();
  return t === '' ? false : t.startsWith('//') || t.startsWith('/*') || t.startsWith('*') || t.endsWith('*/');
}

// The delete range for a managed property: from the start of its own physical
// line (absorbing any whole-line comments directly above it) through the end of
// the line holding its value (absorbing a following comma and same-line trailing
// comment). This removes the managed key and ITS comments while leaving the
// previous key's line (and any trailing comment on it) byte-for-byte intact.
function managedDeleteRange(text, prop) {
  let start = text.lastIndexOf('\n', prop.offset - 1) + 1; // start of prop's line
  // Absorb whole-line comments immediately above (own-line comments only).
  for (;;) {
    if (start === 0) break;
    const prevEnd = start - 1; // the '\n' terminating the previous line
    const prevBegin = text.lastIndexOf('\n', prevEnd - 1) + 1;
    if (isCommentLine(text.slice(prevBegin, prevEnd))) start = prevBegin;
    else break;
  }
  let end = prop.offset + prop.length;
  while (end < text.length && (text[end] === ' ' || text[end] === '\t')) end++;
  if (text[end] === ',') end++;
  const nl = text.indexOf('\n', end);
  end = nl === -1 ? text.length : nl + 1; // consume through end of this line
  return [start, end];
}

if (cmd === 'apply') {
  const [file, profileArg, keyRegex] = args;
  if (!file || !profileArg || !keyRegex) die('apply <liveFile> <profileFileOrNONE> <keyRegex>');
  const re = new RegExp(keyRegex);

  let text = existsSync(file) ? readFileSync(file, 'utf8') : '{}\n';

  // 1. Strip every managed key currently present, surgically (right-to-left so
  //    offsets stay valid). Non-managed keys and their comments are untouched.
  const toStrip = properties(rootObject(text)).filter((p) => re.test(p.children[0].value));
  for (let i = toStrip.length - 1; i >= 0; i--) {
    const [s, e] = managedDeleteRange(text, toStrip[i]);
    text = text.slice(0, s) + text.slice(e);
  }

  // 2. If applying a profile (not clean), re-insert managed keys WITH comments.
  //    The profile object is already cleanly formatted (see extract), so we
  //    splice its inner text verbatim right after the live opening brace. We do
  //    NOT reformat the whole document: that would strip trailing same-line
  //    comments on the user's untouched (non-managed) keys.
  if (profileArg !== 'NONE') {
    if (!existsSync(profileArg)) die(`profile file not found: ${profileArg}`);
    const profText = readFileSync(profileArg, 'utf8');
    const profRoot = rootObject(profText);
    const profProps = properties(profRoot);
    if (profProps && profProps.length) {
      // Inner text between the profile's outer braces (formatted, 2-space).
      const inner = profText
        .slice(profRoot.offset + 1, profRoot.offset + profRoot.length - 1)
        .replace(/^\n+/, '')
        .replace(/\s+$/, '');
      const root = rootObject(text);
      const insertAt = root.offset + 1; // right after the live opening brace
      const hasExisting = properties(root).length > 0;
      const block = '\n' + inner + (hasExisting ? ',' : '');
      text = text.slice(0, insertAt) + block + text.slice(insertAt);
      // Tidy any blank-line gaps the strip step may have left behind.
      text = text.replace(/\n[ \t]*\n[ \t]*\n/g, '\n\n');
    }
  }

  writeFileSync(file, text);
  process.exit(0);
}

die(`unknown command: ${cmd ?? '(none)'} (expected extract|apply)`);
