"""Target registry for agent evaluation targets."""

from dataclasses import dataclass
from typing import Any, Callable

_registry: dict[str, "RegisteredTarget"] = {}


class TargetRegistrationError(Exception):
    """Raised when registering a target with a name that already exists."""


class TargetNotFoundError(Exception):
    """Raised when looking up a target that does not exist."""


@dataclass
class RegisteredTarget:
    """A registered target function with its metadata."""

    name: str
    version: str
    func: Callable[[Any, dict[str, Any]], Any]


def target(name: str, version: str) -> Callable[[Callable], Callable]:
    """Decorator that registers a function as an evaluation target.

    Args:
        name: Unique name for the target.
        version: Version string for the target.

    Returns:
        A decorator that registers the function and returns it unchanged.

    Raises:
        TargetRegistrationError: If a target with the same name is already registered.
    """

    def decorator(func: Callable[[Any, dict[str, Any]], Any]) -> Callable:
        if name in _registry:
            msg = f"Target '{name}' is already registered"
            raise TargetRegistrationError(msg)
        rt = RegisteredTarget(name=name, version=version, func=func)
        _registry[name] = rt
        return func

    return decorator


def get_target(name: str) -> RegisteredTarget:
    """Look up a registered target by name.

    Args:
        name: The name of the target to look up.

    Returns:
        The registered target.

    Raises:
        TargetNotFoundError: If no target with the given name is registered.
    """
    if name not in _registry:
        msg = f"Target '{name}' is not registered"
        raise TargetNotFoundError(msg)
    return _registry[name]


# ---- Internal helpers for testing ----

def _clear_registry() -> None:
    _registry.clear()


def _save_registry() -> dict[str, RegisteredTarget]:
    return dict(_registry)


def _restore_registry(saved: dict[str, RegisteredTarget]) -> None:
    _registry.update(saved)
