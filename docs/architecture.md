# Manafield Manage Architecture

- `modules/manage-web`: read-only instance observation (Registry, Resources, Capabilities).
- `modules/manage-lifecycle` (planned): request authorized rebuild / restart / update operations via a privileged deployment controller, not direct Docker access.
- `modules/manage-monitor` and `modules/manage-audit` (planned): monitoring and operation history.

The core CLI / deployment engine remains usable if Manage fails. Manage Web must never receive a Docker socket or Jenkins credentials.

The legacy local `manafield-manage` instance module remains deployed unchanged until a deliberate migration to `manafield-manage-web` is validated.
