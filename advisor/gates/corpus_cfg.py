#!/usr/bin/env python3
"""corpus_cfg.py: per-repo configuration overlay for the corpus gate runner (run3.py --corpus-config FILE).

The overlay is applied to a COPY of a repo (the run clone), never to the source clone. What Glade actually reads
(docs/CONFIG.md and internal/project/project.go on codex/next-integration-20261020; `dependencyPaths` resolves declared package source projects):
  * package directories: sfdx-project.json `packageDirectories[].path` (or glade.yml `project.packageDirs`, which replaces them).
    An unmanaged dependency is wired by appending its source dirs as extra package directories of the copy
    (`.glade-deps/<dep>/<dir>`). This is what a deploy of both source trees to one org looks like.
  * managed package source: glade.yml `project.managedPackageDependencies: ["<namespace>:<relative path>"]`
    (the namespace is required and is applied to the loaded source; use it only for a real namespaced package, e.g. dlrs).
  * org features: glade.yml `org.features: [PersonAccounts, ...]` (union with config/project-scratch-def.json features).
Dependency test classes become part of the project in the packageDirectories mode, so apply() returns their class names
and the runner subtracts them from the test selection (only the repo's own tests are counted).

corpus-config.json:
  {"depSources": {"<dep>": {"paths": ["~/a", "~/b"], "dirs": ["sub/dir"], "repoPath": "relative/nested/project", "packages": ["declared-package"], "mode": "source"|"managed"|"stub"|"project", "markDefault": ["dir", ...] (managed: set default:true on these dirs; Glade loads only default dirs),
                            "namespace": "<ns>"  (managed; default: sfdx-project.json namespace), "note": "..."}},
   "repos": {"<repo dir name>": {"deps": ["<dep>", ...], "features": ["PersonAccounts"], "projectNamespace": "" (override the manifest namespace for repos whose CI runs unmanaged), "permissionSets": ["<api name>"] (glade.yml test.permissionSets), "userLocale": "en_CA" (glade.yml test.user.locale), "note": "...", "disabled": true}}}
A dep whose paths all miss is recorded as unavailable and skipped (the repo still runs, with the rest of its overlay).
  * installed packages without source (v0.3.5 ruling 49(1)): repo key "externalNamespaces": ["ns", ...] writes glade.yml
    `project.externalNamespaces: [ns, ...]`. Binaries before the namespace family reject that key, so it is emitted only when the
    config's top-level "emitExternalNamespaces" is true (otherwise the record lists them under externalNamespacesPending).
Python 3.9 compatible.
"""
import argparse, hashlib, json, os, platform, re, shutil, subprocess, sys
from pathlib import Path

DEPS_DIR = '.glade-deps'


def load_config(path):
    cfg = json.loads(Path(path).read_text())
    cfg.setdefault('depSources', {}); cfg.setdefault('repos', {})
    return cfg


def _read_json(path):
    return json.loads(Path(path).read_text())


def _norm(p):
    p = p.strip().replace('\\', '/')
    while p.startswith('./'):
        p = p[2:]
    return p.rstrip('/') or '.'


def resolve_dep(name, cfg, root=None):
    spec = cfg['depSources'].get(name)
    if not spec:
        return None, None
    if root is not None and spec.get('repoPath'):
        p = (Path(root) / spec['repoPath']).resolve()
        if not p.is_relative_to(Path(root).resolve()):
            raise ValueError('repoPath must stay inside the run copy')
        return (p if p.is_dir() else None), spec
    for cand in spec.get('paths', []):
        p = Path(os.path.expanduser(cand))
        if p.is_dir():
            return p, spec
    return None, spec


def _dep_dirs(src, spec):
    if spec.get('dirs'):
        return [_norm(d) for d in spec['dirs']]
    try:
        dirs = [_norm(d['path']) for d in _read_json(src / 'sfdx-project.json')['packageDirectories']]
        if dirs:
            return dirs
    except Exception:
        pass
    return ['.']


def _copy_tree(src, dst):
    """Copy a directory without .git/node_modules, using clonefile/reflink when the filesystem has it."""
    ignore = shutil.ignore_patterns('.git', 'node_modules', '.sfdx', '.sf')
    if dst.exists():
        shutil.rmtree(str(dst))
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(str(src), str(dst), ignore=ignore, symlinks=True)


def _classes_under(root):
    names = set()
    for f in Path(root).rglob('*.cls'):
        names.add(f.stem)
    return names


def _yaml_list(items):
    for i in items:
        if ',' in i or '[' in i or ']' in i or '"' in i:
            raise ValueError('path not representable in a glade.yml inline list: %r' % i)
    return '[' + ', '.join(items) + ']'


def apply_overlay(repo, root, cfg, log=lambda m: None):
    """Apply cfg['repos'][repo] to the copy at ROOT. Returns (record, exclude_class_names)."""
    root = Path(root)
    entry = cfg['repos'].get(repo)
    rec = {'repo': repo, 'applied': False, 'deps': [], 'features': [], 'warnings': []}
    if not entry:
        rec['reason'] = 'no overlay for this repo'
        return rec, set()
    rec['note'] = entry.get('note')
    if entry.get('disabled'):
        rec['reason'] = 'disabled in corpus-config'
        return rec, set()
    exclude = set()
    extra_dirs = []        # package directories to append (relative to root)
    dependency_paths = []
    managed = []           # "ns:path" strings
    for dep in entry.get('deps', []):
        spec = cfg['depSources'].get(dep, {})
        if spec.get('mode') == 'stub':
            # Empty source projects acknowledge declarations only, never package behavior.
            packages = spec.get('packages', [])
            declared = {d['package'] for pd in _read_json(root / 'sfdx-project.json').get('packageDirectories', [])
                        for d in pd.get('dependencies', []) if 'package' in d}
            if not packages or not set(packages) <= declared:
                raise ValueError('stub packages must be explicitly declared by the consumer')
            dest = root / DEPS_DIR / dep
            dirs = []
            for index, package in enumerate(packages):
                sub = 'src%d' % index
                (dest / sub).mkdir(parents=True, exist_ok=True)
                dirs.append({'path': sub, 'default': True, 'package': package})
            (dest / 'sfdx-project.json').write_text(json.dumps({'namespace': '', 'packageDirectories': dirs}, indent=2) + '\n')
            dependency_paths.append('%s/%s' % (DEPS_DIR, dep))
            rec['deps'].append({'name': dep, 'mode': 'stub', 'status': 'stub', 'packages': packages,
                                'reason': 'declaration-only empty source; package behavior is unavailable'})
            continue
        src, spec = resolve_dep(dep, cfg, root)
        d = {'name': dep, 'mode': (spec or {}).get('mode', 'source')}
        if src is None:
            d['status'] = 'unavailable'
            d['reason'] = (spec or {}).get('note') or ('no source path exists for %r' % dep)
            rec['deps'].append(d); rec['warnings'].append('dep %s unavailable' % dep)
            continue
        dest = root / DEPS_DIR / dep
        d['src'] = str(src)
        if d['mode'] == 'managed':
            _copy_tree(src, dest)
            ns = spec.get('namespace')
            if not ns:
                try:
                    ns = _read_json(dest / 'sfdx-project.json').get('namespace') or ''
                except Exception:
                    ns = ''
            if not ns:
                d['status'] = 'error'; d['reason'] = 'managed mode needs a namespace'
                rec['deps'].append(d); rec['warnings'].append('dep %s has no namespace' % dep)
                continue
            managed.append('%s:%s/%s' % (ns, DEPS_DIR, dep))
            d['namespace'] = ns
            # Glade loads only the `default: true` package directories of a source dependency; mark the ones the code lives in
            mark = spec.get('markDefault')
            if mark:
                mp = dest / 'sfdx-project.json'
                md = _read_json(mp)
                want = {_norm(x) for x in mark}
                for pd in md.get('packageDirectories', []):
                    if _norm(pd.get('path', '')) in want:
                        pd['default'] = True
                mp.write_text(json.dumps(md, indent=2) + '\n')
                d['markDefault'] = sorted(want)
        elif d['mode'] == 'project':
            # A whole source project that DECLARES the requested packages (its sfdx-project.json `package` entries).
            # Glade resolves declared SFDX dependencies through glade.yml project.dependencyPaths, so the copy is
            # listed there and is not appended to packageDirectories (its test classes are never selected).
            sib = root.parent / Path(src).name
            if sib.is_dir() and (sib / 'sfdx-project.json').exists() and sib.resolve() != root.resolve():
                # rc1fix: the run root already holds this project as a sibling checkout; Glade's sibling scan finds
                # it, and a second copy under .glade-deps would make the declared package dependency_ambiguous.
                d['sibling'] = sib.name
            else:
                _copy_tree(src, dest)
                dependency_paths.append('%s/%s' % (DEPS_DIR, dep))
        else:
            dirs = _dep_dirs(src, spec)
            for sub in dirs:
                s = src / sub if sub != '.' else src
                if not s.is_dir():
                    d.setdefault('missingDirs', []).append(sub)
                    continue
                t = dest / sub if sub != '.' else dest
                _copy_tree(s, t)
                rel = '%s/%s%s' % (DEPS_DIR, dep, '' if sub == '.' else '/' + sub)
                extra_dirs.append(rel)
                exclude |= _classes_under(t)
            d['dirs'] = dirs
        d['status'] = 'applied'
        rec['deps'].append(d)
    if 'projectNamespace' in entry:
        # Run the repo the way its own CI does (e.g. an unmanaged, namespace-less scratch org): override the manifest namespace.
        sfdx = root / 'sfdx-project.json'
        try:
            data = _read_json(sfdx)
            rec['namespaceBefore'] = data.get('namespace')
            data['namespace'] = entry['projectNamespace']
            sfdx.write_text(json.dumps(data, indent=2) + '\n')
            rec['namespaceOverride'] = entry['projectNamespace']
        except Exception as e:
            rec['warnings'].append('projectNamespace not applied: %s' % type(e).__name__)
    features = list(entry.get('features', []))
    permsets = list(entry.get('permissionSets', []))
    user_locale = entry.get('userLocale')
    rec['features'] = features
    # sfdx-project.json: append the extra package directories (keeps the repo's own dependencies/aliases/namespace)
    sfdx = root / 'sfdx-project.json'
    if extra_dirs:
        if sfdx.exists():
            try:
                data = _read_json(sfdx)
                dirs = data.setdefault('packageDirectories', [])
                have = {_norm(x.get('path', '')) for x in dirs}
                rec['sfdxProjectSha256Before'] = hashlib.sha256(sfdx.read_bytes()).hexdigest()[:16]
                for rel in extra_dirs:
                    if rel not in have:
                        dirs.append({'path': rel})
                sfdx.write_text(json.dumps(data, indent=2) + '\n')
                rec['mechanism'] = 'sfdx-project.json packageDirectories append'
                rec['appendedPackageDirectories'] = extra_dirs
            except Exception as e:      # comments or trailing commas in the manifest
                rec['warnings'].append('sfdx-project.json not rewritten: %s' % type(e).__name__)
                rec['mechanism'] = 'none (sfdx-project.json unreadable)'
        else:
            rec['warnings'].append('no sfdx-project.json: package dirs written to glade.yml as [., extras]')
            rec['mechanism'] = 'glade.yml project.packageDirs'
    # glade.yml: managed deps, features, (packageDirs only when there is no manifest)
    lines = []
    proj = []
    if dependency_paths:
        proj.append('  dependencyPaths: ' + _yaml_list(dependency_paths))
    if managed:
        proj.append('  managedPackageDependencies: ' + '[' + ', '.join('"%s"' % m for m in managed) + ']')
    external = [str(n).strip() for n in entry.get('externalNamespaces', []) if str(n).strip()]
    if external:
        if cfg.get('emitExternalNamespaces'):
            proj.append('  externalNamespaces: ' + _yaml_list(external))
            rec['externalNamespaces'] = external
        else:
            rec['externalNamespacesPending'] = external
    if extra_dirs and not sfdx.exists():
        proj.append('  packageDirs: ' + _yaml_list(['.'] + extra_dirs))
    if proj:
        lines.append('project:'); lines += proj
    if features:
        lines += ['org:', '  features: ' + _yaml_list(features)]
    if permsets or user_locale:
        lines += ['test:']
        if permsets:
            lines += ['  permissionSets: ' + _yaml_list(permsets)]
        if user_locale:
            lines += ['  user:', '    locale: ' + str(user_locale)]
    gy = root / 'glade.yml'
    if gy.exists() and rec.get('externalNamespaces'):
        # The repo's own glade.yml stays; only the namespace declaration is merged into its project section.
        text = gy.read_text()
        rows = text.splitlines()
        if 'project:' in rows and not any(r.strip().startswith('externalNamespaces:') for r in rows):
            at = rows.index('project:') + 1
            rows.insert(at, '  externalNamespaces: ' + _yaml_list(rec['externalNamespaces']))
            gy.write_text('\n'.join(rows) + '\n')
            rec['gladeYmlMerged'] = 'project.externalNamespaces'
        elif 'project:' not in rows:
            gy.write_text(text.rstrip('\n') + '\nproject:\n  externalNamespaces: ' + _yaml_list(rec['externalNamespaces']) + '\n')
            rec['gladeYmlMerged'] = 'project.externalNamespaces'
        else:
            rec['warnings'].append('glade.yml already declares externalNamespaces; overlay namespaces NOT merged')
        lines = [l for l in lines if not l.startswith('  externalNamespaces: ')]
        if lines == ['project:']:
            lines = []
    if lines:
        if gy.exists():
            rec['warnings'].append('glade.yml already exists; managed deps / features NOT applied')
        else:
            gy.write_text('\n'.join(lines) + '\n')
            rec['gladeYml'] = '\n'.join(lines)
    rec['applied'] = bool(rec.get('appendedPackageDirectories') or 'namespaceOverride' in rec or ('gladeYml' in rec and (managed or dependency_paths or features or permsets or user_locale or extra_dirs or rec.get('externalNamespaces'))) or 'gladeYmlMerged' in rec)
    rec['excludeClassCount'] = len(exclude)
    return rec, exclude


def main():
    a = argparse.ArgumentParser(description='apply a corpus-config overlay to one repo copy')
    a.add_argument('--config', required=True); a.add_argument('--repo', required=True)
    a.add_argument('--root', required=True, help='the COPY of the repo'); a.add_argument('--record')
    a.add_argument('--excludes', help='write the dep class names (one per line) here')
    args = a.parse_args()
    rec, ex = apply_overlay(args.repo, args.root, load_config(args.config))
    if args.record:
        Path(args.record).write_text(json.dumps(rec, indent=1, sort_keys=True))
    if args.excludes:
        Path(args.excludes).write_text('\n'.join(sorted(ex)) + '\n')
    print(json.dumps(rec, indent=1, sort_keys=True))


if __name__ == '__main__':
    main()
