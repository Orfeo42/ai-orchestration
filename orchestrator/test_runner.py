import subprocess
from pathlib import Path


def run_tests(project_path: str) -> tuple[bool, str]:
    path = Path(project_path)

    if (path / "go.mod").exists():
        cmd = ["go", "test", "./..."]
    elif (path / "pytest.ini").exists() or (path / "setup.cfg").exists():
        cmd = ["pytest", "--tb=short", "-q"]
    elif (path / "pyproject.toml").exists() and _has_pytest(path):
        cmd = ["pytest", "--tb=short", "-q"]
    elif (path / "package.json").exists():
        cmd = ["npm", "test", "--", "--watchAll=false", "--passWithNoTests"]
    else:
        return True, ""

    result = subprocess.run(
        cmd,
        cwd=path,
        capture_output=True,
        text=True,
        timeout=180,
    )

    if result.returncode != 0:
        output = (result.stdout + result.stderr)[:3000]
        return False, output

    return True, ""


def _has_pytest(path: Path) -> bool:
    content = (path / "pyproject.toml").read_text()
    return "pytest" in content
