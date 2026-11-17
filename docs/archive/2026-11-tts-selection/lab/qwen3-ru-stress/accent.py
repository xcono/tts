#!/usr/bin/env python3
"""Mark Russian stress with RUAccent and convert to U+0301 after the vowel.

Model-card advice applied: Title-case words are lowercased before RUAccent
(it stresses capitalised words worse), then the original case is restored.
No mark on one-syllable words or on «ё» (already unambiguous).

Usage: ~/.cache/venvs/ruaccent/bin/python accent.py < text.txt > marked.txt
Latin words pass through untouched.
"""
import os
import re
import sys

from ruaccent import RUAccent

VOWELS = "аеёиоуыэюяАЕЁИОУЫЭЮЯ"
ACUTE = "́"


def plus_to_acute(word: str) -> str:
    """'+' before vowel -> U+0301 after vowel; drop the mark where it is redundant."""
    plain = word.replace("+", "")
    nvow = sum(c in VOWELS for c in plain)
    if "+" not in word:
        return plain
    i = word.index("+")
    v = word[i + 1] if i + 1 < len(word) else ""
    if nvow <= 1 or v in "ёЁ":
        return plain
    return word[:i] + v + ACUTE + word[i + 2:]


def main() -> None:
    text = sys.stdin.read().strip()
    acc = RUAccent()
    acc.load(omograph_model_size=os.environ.get("RUACCENT_OMOGRAPH", "turbo3"), use_dictionary=True, tiny_mode=False,
             workdir=os.path.expanduser("~/.cache/ruaccent"))
    # lowercase Title-case Cyrillic words, remember them to restore case after
    toks = re.split(r"(\s+)", text)
    titled = [bool(re.match(r"^[А-ЯЁ][а-яё]", t)) for t in toks]
    low = "".join(t[0].lower() + t[1:] if f else t for t, f in zip(toks, titled))
    marked = acc.process_all(low)
    out = re.sub(r"[А-Яа-яЁё+]+", lambda m: plus_to_acute(m.group(0)), marked)
    # restore capitals: same token order since only '+' was inserted
    otoks = re.split(r"(\s+)", out)
    if len(otoks) == len(toks):
        otoks = [t[0].upper() + t[1:] if f else t for t, f in zip(otoks, titled)]
    print("".join(otoks))
    print(marked, file=sys.stderr)


if __name__ == "__main__":
    main()
