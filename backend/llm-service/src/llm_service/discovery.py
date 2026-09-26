"""Bounded repository-local README link discovery. Never fetch external URLs."""
import posixpath
import re
from urllib.parse import unquote, urlsplit

from .sampling import file_priority

EXTENSIONS = ('.md', '.rst', '.adoc', '.txt')
IGNORED = {'.git', '.venv', 'node_modules', 'vendor', 'dist', 'build', 'target', '.cache'}


def readme_links(files, all_paths, limit=8):
    found = {}
    for source in sorted(files, key=file_priority):
        if not source.lower().rsplit('/', 1)[-1].startswith('readme'):
            continue
        text = re.sub(r'(?ms)^\s*(`{3,}|~{3,}).*?^\s*\1\s*$', '', files[source])
        refs = {name.casefold(): target for name, target in re.findall(
            r'^\s*\[([^]]+)\]:\s*<?([^\s>]+)>?', text, re.M)}
        links = re.findall(r'(?<!!)\[([^]]+)\]\(\s*<?([^\s)>]+)>?(?:\s+[^)]*)?\)', text)
        for label, ref in re.findall(r'(?<!!)\[([^]]+)\]\[([^]]*)\]', text):
            if (ref or label).casefold() in refs:
                links.append((label, refs[(ref or label).casefold()]))
        for label, target in links[:100]:
            try:
                url = urlsplit(target)
            except ValueError:
                continue
            if url.scheme or url.netloc or not url.path:
                continue
            path = unquote(url.path)
            if '\\' in path or '\x00' in path:
                continue
            path = posixpath.normpath(path.lstrip("/") if path.startswith("/") else
                                     posixpath.join(posixpath.dirname(source), path))
            if (path.startswith(('/', '../')) or path not in all_paths or
                    not path.lower().endswith(EXTENSIONS) or
                    any(p.lower() in IGNORED for p in path.split('/')[:-1])):
                continue
            found.setdefault(path, label[:200])
    return dict(sorted(found.items(), key=lambda item: file_priority(item[0]))[:limit])
