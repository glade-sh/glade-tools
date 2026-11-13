"""Compare two check JSON outputs without hiding diagnostic changes."""
import difflib
import json


def canonical(path):
    data = json.loads(path.read_text())
    if not isinstance(data, dict) or not isinstance(data.get('diagnostics'), list):
        raise ValueError('missing diagnostics array')
    # Diagnostic order is not semantic; preserve duplicates and every field.
    data['diagnostics'] = sorted(data['diagnostics'], key=lambda d: json.dumps(d, sort_keys=True, ensure_ascii=False))
    return json.dumps(data, sort_keys=True, indent=2, ensure_ascii=False).splitlines(keepends=True)


def compare(parallel, serial, parallel_exit, serial_exit, procs, diff_path):
    receipt = {'parallel_procs': procs, 'parallel_exit': parallel_exit, 'serial_exit': serial_exit}
    try:
        if int(procs) <= 1 or parallel_exit not in (0, 1) or serial_exit not in (0, 1):
            raise ValueError('comparison requires parallel workers and two completed checks')
        a, b = canonical(parallel), canonical(serial)
        diff = ''.join(difflib.unified_diff(b, a, fromfile='serial', tofile='parallel'))
        diff_path.write_text(diff)
        receipt['verdict'] = 'match' if not diff and parallel_exit == serial_exit else 'mismatch'
    except (ValueError, TypeError, OSError) as error:
        receipt['verdict'] = 'incomplete'
        receipt['reason'] = str(error)
        diff_path.unlink(missing_ok=True)
    return receipt
