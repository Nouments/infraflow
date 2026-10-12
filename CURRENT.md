# InfraFlow Current Status

Updated: 2026-10-12
Branch: `develop`
Base Commit: `b2c65cce4d0ad76c1514817e4de1285bc0d3856f`

## Implemented and verified in the repository

- Provider validation and deterministic planning are separate from generation and execution. Creating or retrying a job does not provision infrastructure.
- Planning jobs are persisted as private JSON records; the authenticated API supports list, read, cancel, and retry within the implemented status rules.
- A separate execution aggregate persists queued/running/succeeded/failed/cancelled/timed_out state, tasks, attempts, outputs, errors, actors, and transition history. It uses a typed executor port and the existing scheduler.
- Admin HTTP routes submit with idempotency, list with filters/pagination, read attempts/results/events, and request cancellation. The default Provider registers no executor and rejects unsupported types instead of reporting a false success.
- Artifact generation publishes deterministic, hash-verified catalog entries. Vendor artifacts remain marked unverified; execution and verification are not requested.
- The Agent downloads published artifacts, verifies the stream and content hashes, stores them locally, processes generic inventory/topology, and reports artifact-level outcomes.
- The Agent has a local log outbox and can synchronize logs over the authenticated provider contract.
- The scheduler implements dependency graphs, global concurrency, resource locks, cancellation, retries, and timeouts. Provider startup wires it to the persistent execution service; no device executor is registered.
- Bootstrap artifacts can be generated. Agent DHCPv4, read-only TFTP, and bootstrap HTTP servers are separate explicit commands; they are not started by the normal agent run. There is no DNS daemon.
- Authenticated provider HTTP/gRPC APIs expose their existing planning, agent, event, and log records. The execution API is admin-only; administrators can inspect paginated execution lists, view task/attempt/error details, and request audited cancellation from both the web dashboard and the Linux TUI.

## Evidence from Task 1 (Fiabilisation des états TUI et des contrôles locaux)

- **Sous-tâche A — Stockage réel des artefacts (`StateDirectory`)** :
  - L'adaptateur réel `filesystem.StateStore` utilise directement `settings.Agent.StateDirectory` comme racine pour créer les dossiers `<site>/...` et `.infraflow-agent-state.json`. Il n'existe aucun sous-dossier intermédiaire `artifacts/`.
  - `inspectArtifactStore` inspecte fidèlement `StateDirectory` :
    - Répertoire inexistant : `Absent` (`Répertoire inexistant (non initialisé sur le disque)`).
    - Chemin pointant vers un fichier : `Dégradé` (`Le chemin configuré pointe vers un fichier au lieu d'un répertoire`).
    - Répertoire inaccessible : `Inaccessible` (`Erreur d'accès / refusé`).
    - Répertoire existant : `Opérationnel` avec mention `Accessible (N entrées stockées)`.
    - Intégrité : explicitement `Non auditée par le TUI (intégrité SHA256 validée par l'agent lors des réceptions gRPC)`.

- **Sous-tâche B — Statut véridique des outils Ansible et Terraform** :
  - La présence d'exécutables dans le `PATH` ne prouvant pas leur bon fonctionnement, les outils détectés dans `PATH` sans test d'exécution affichent désormais `Détecté` (`ansible-playbook et terraform détectés dans PATH (fonctionnement non vérifié)`).
  - Si un outil est manquant : `Dégradé` avec mention de l'exécutable manquant.
  - Si les deux sont manquants : `Absent` (`introuvables dans PATH`).
  - Aucun faux statut `Opérationnel` n'est affirmé sans exécution réelle.

- **Sous-tâche C — Clarification du cycle de vie de l'Outbox** :
  - `observability.NewOutbox` ne crée pas le fichier `pending-events.json` tant qu'aucun événement n'est persisté (`outbox.persist()`).
  - L'absence du fichier avant premier run est rapportée comme `Non initialisé` (`Fichier pending-events.json non créé (aucune file persistée sur disque)`), sans prétendre à tort que la file est vide.
  - Lorsque le fichier existe, son contenu est inspecté et décodé : `Opérationnel` (`File vérifiée (fichier vide, 0 événement en attente)` ou `N événement(s) en attente`), `Dégradé` si JSON corrompu, `Inaccessible` si illisible.

- **Sous-tâche D — Garantie des erreurs de configuration sur tous les modes CLI/TUI** :
  - Couverture systématique des modes `run`, `tui`, `execute-ansible` et `validate-terraform`.
  - Rejet systématique avec code de sortie non-nul (2) et message explicite sur `stderr` (avec masquage des secrets) pour :
    - Fichier de configuration absent / introuvable.
    - YAML syntaxiquement invalide.
    - Champ inconnu rejeté par `decoder.KnownFields(true)`.
    - Configuration incomplète rejetée par `Validate()`.

- **Sous-tâche E — Cycle de vie et résilience hors-ligne du TUI** :
  - Validation du cycle complet en 4 phases :
    1. Aucune donnée reçue au démarrage : `Indisponible (aucune donnée reçue)`, horodatage `Aucune synchronisation`.
    2. Première synchronisation réussie : `● Opérationnel`, affichage des données et de l'horodatage de rafraîchissement.
    3. Perte de connexion : conservation stricte des données en mémoire vive de session, maintien de l'horodatage initial réussi (sans inventer d'actualisation), affichage explicite de l'indisponibilité et de `Données en mémoire de session (non persistées sur disque)`.
    4. Reconnexion réussie : mise à jour des données, nouvel horodatage et rétablissement du statut `Synchronisé`.

- **Vérifications et Tests exécutés** :
  - `make fmt-check` : code 0, 100% conforme `gofmt`.
  - `make vet` : code 0, aucune erreur.
  - `make test-agent` : code 0, 100% des tests de l'agent passent.
  - `go test -count=1 ./...` : code 0, 100% des tests du dépôt passent sans cache.
  - `make build` : code 0, compilation réussie de `bin/infraflow-provider` et `bin/infraflow-agent`.

## Remaining work

- Register a real, explicitly authorized executor only after defining its allowed task type, parameter schema, privilege boundary, and independent evidence. Until then all unregistered execution types remain rejected.
- Implement or explicitly defer a DNS service and other unimplemented bootstrap services.
- Run the final full test suite and binary builds in the separate final campaign. Docker and GNS3 validation remain separate deployment gates.

## Next implementation task

Define and authorize the first operational task executor adapter with its strict safety policy, schema validation, and acceptance evidence. Do not register vendor executors without real lab verification.