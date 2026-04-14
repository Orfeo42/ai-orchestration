import subprocess
from pathlib import Path

import structlog

log = structlog.get_logger(__name__)


def run_tests(project_path: str) -> tuple[bool, str]:
    path = Path(project_path)
    cmd = None

    if (path / "go.mod").exists():
        cmd = ["go", "test", "-short", "./..."]
    elif (path / "pytest.ini").exists() or (path / "setup.cfg").exists():
        cmd = ["pytest", "--tb=short", "-q"]
    elif (path / "pyproject.toml").exists() and _has_pytest(path):
        cmd = ["pytest", "--tb=short", "-q"]
    elif (path / "package.json").exists():
        cmd = ["npm", "test", "--", "--watchAll=false", "--passWithNoTests"]
    
    if cmd is None:
        log.debug("no test runner detected", project_path=project_path)
        return True, ""

    log.info("running tests", project_path=project_path, command=" ".join(cmd))
    
    try:
        result = subprocess.run(
            cmd,
            cwd=path,
            capture_output=True,
            text=True,
            timeout=180,
        )

        if result.returncode != 0:
            log.warning("tests failed", code=result.returncode, project_path=project_path)
            output = (result.stdout + result.stderr)[:3000]
            return False, output

        log.info("tests passed", project_path=project_path)
        return True, ""
    except subprocess.TimeoutExpired:
        log.error("tests timed out", project_path=project_path)
        return False, "Tests timed out after 180 seconds"
    except Exception as exc:
        log.error("test execution failed", error=str(exc), project_path=project_path)
        return False, str(exc)


def _has_pytest(path: Path) -> bool:
    content = (path / "pyproject.toml").read_text()
    return "pytest" in content
