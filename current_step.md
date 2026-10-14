# INFRAFLOW — BLOC 2 : TESTS COMPLETS ET VALIDATION TECHNIQUE

## 1. CONTEXTE ET OBJECTIF

Dépôt : `Nouments/infraflow`  
Branche : `develop`  
Dernier commit connu : `3df0215185fec2e5f174a238bd257ec4250a7da1`  
Fichier de suivi : `current_step.md`

Tu es responsable de l'exécution des tests, du diagnostic des échecs et des corrections strictement nécessaires au **Bloc 2 : tests complets et validation technique**.

L'objectif est de vérifier la cohérence du dépôt après les changements du Bloc 1, d'identifier les défauts reproductibles et de les corriger sans introduire de nouvelles fonctionnalités.

**Important :** le Bloc 1 n'a pas encore reçu l'approbation finale PO/QA. Cela ne t'empêche pas d'exécuter le Bloc 2. Ne modifie pas artificiellement son statut pour le déclarer terminé.

## 2. RÈGLES STRICTES

1. Reste sur `develop`.
2. Vérifie le HEAD et le statut Git avant de modifier quoi que ce soit.
3. Inspecte `current_step.md`, `CURRENT.md`, les instructions du dépôt et les commandes de validation réellement disponibles.
4. Préserve toutes les modifications préexistantes.
5. N'utilise jamais `git reset --hard`, `git clean -fd` ou une commande destructive.
6. Ne change pas l'architecture pour des raisons esthétiques.
7. Ne supprime pas, ne désactive pas et n'affaiblis pas un test pour obtenir un résultat positif.
8. Ne remplace jamais un test réel par un test fictif ou un résultat supposé.
9. Pour chaque échec, identifie la cause avant de corriger.
10. Après chaque correction, relance le test concerné.
11. N'effectue aucune opération de déploiement sur un équipement réseau.
12. N'exécute aucun `terraform apply`.
13. Ne commence pas le Bloc 3 du projet et n'effectue aucun push sans autorisation explicite.
14. Si une dépendance manque, consigne `BLOCKED` avec la commande et l'erreur réelle.
15. Ne prétends jamais qu'une validation a réussi si elle n'a pas été exécutée.

## 3. BLOC 2.1 — ÉTAT INITIAL DU DÉPÔT

Exécute et conserve les résultats de :

```bash
git branch --show-current
git rev-parse HEAD
git status --short --branch
git log -5 --oneline
git diff --check
```

Confirme que tu es sur `develop` et identifie le HEAD exact.

Si le HEAD diffère du commit connu, inspecte les nouveaux commits avant de continuer. Ne réinitialise pas le dépôt pour revenir à un ancien commit.

Identifie les instructions du projet, les cibles Makefile et les commandes de test disponibles. N'invente aucune cible.

## 4. BLOC 2.2 — TESTS GO COMPLETS

Commence par les tests ciblés des composants modifiés récemment, notamment :

- `pkg/protocol`
- `internal/adapters/generation`
- `provider/internal/adapters/filesystem`
- `provider/internal/adapters/executors`
- `provider/internal/delivery/grpcapi`
- `agent/internal/adapters/providergrpc`
- `agent/internal/delivery/artifacts`

Adapte les chemins aux packages réellement présents dans le dépôt.

Exécute ensuite :

```bash
go test -count=1 ./...
make fmt-check
make vet
make build
git diff --check
```

Pour chaque commande :

- conserve la commande exacte ;
- conserve le code de sortie ;
- distingue `PASS`, `FAIL`, `BLOCKED` et `NOT RUN` ;
- conserve les erreurs pertinentes ;
- identifie le package ou le fichier à l'origine de l'échec.

Si une cible Makefile n'existe pas, ne la crée pas uniquement pour faire passer la mission. Utilise les instructions du dépôt pour identifier l'équivalent approprié et explique la différence.

Si les tests échouent, diagnostique et corrige les défauts du code qui relèvent réellement du périmètre. Ne modifie pas les assertions pour masquer une régression.

Lorsque c'est pertinent et réalisable, exécute également :

```bash
go test -race -count=1 ./...
```

Si le test de concurrence est trop coûteux ou impossible dans l'environnement disponible, indique-le explicitement.

## 5. BLOC 2.3 — CONTRÔLE DES ARTEFACTS ET DES RÉGRESSIONS

Vérifie particulièrement les comportements suivants :

- La limite commune de taille des artefacts est cohérente entre les composants concernés.
- Un artefact trop volumineux est refusé.
- Un artefact à la limite autorisée est traité correctement.
- Une erreur de lecture ou d'intégrité n'est pas transformée en succès.
- Le transfert gRPC ne valide pas un artefact tronqué ou altéré.
- Le serveur HTTP ne transmet pas un contenu dépassant sa limite.
- Les erreurs sont propagées de façon cohérente.
- Les tests ne reposent pas sur des équipements fictifs présentés comme des équipements réels.
- Les résultats d'exécution et les rapports ne déclarent pas une opération réussie sans preuve suffisante.

Corrige uniquement les défauts confirmés. Ajoute un test de régression lorsqu'un défaut est découvert et qu'un test pertinent peut le reproduire.

Ne réécris pas tous les composants et ne modifie pas les API publiques sans nécessité démontrée.

## 6. BLOC 2.4 — VALIDATION ANSIBLE

Vérifie d'abord la disponibilité réelle des outils :

```bash
ansible-playbook --version
ansible-galaxy collection list
```

Inspecte les playbooks, inventaires, variables et collections réellement générés par InfraFlow.

Si les outils et dépendances sont disponibles :

1. Génère les artefacts à partir d'une entrée de test explicite.
2. Exécute `ansible-playbook --syntax-check` sur les playbooks applicables.
3. Vérifie les dépendances et les références aux collections.
4. Vérifie la cohérence des inventaires et des variables.
5. N'applique aucune configuration sur un équipement.

Un YAML valide ne prouve pas qu'un playbook fonctionne sur un équipement. Le mode check, s'il est pertinent, ne prouve pas non plus qu'une configuration peut être appliquée réellement.

Si Ansible ou une collection nécessaire manque, indique `BLOCKED` avec le diagnostic exact. Ne simule pas le résultat.

## 7. BLOC 2.5 — VALIDATION TERRAFORM

Vérifie :

```bash
terraform version
```

Inspecte les fichiers Terraform réellement produits par InfraFlow.

Si Terraform est disponible et que les fichiers sont adaptés, valide-les dans un répertoire temporaire isolé :

```bash
terraform fmt -check
terraform init -backend=false
terraform validate
```

Adapte le répertoire de travail aux artefacts réellement générés et protège les fichiers de l'utilisateur.

**Interdiction absolue : exécuter `terraform apply`.**

Précise si InfraFlow génère de véritables ressources de provisionnement ou seulement des données, variables et fichiers de configuration.

Si Terraform est absent ou si les fichiers ne permettent pas la validation, indique la limitation exacte. Ne fabrique pas un provider ou des ressources pour obtenir artificiellement un succès.

## 8. BLOC 2.6 — CORRECTIONS ET NON-RÉGRESSION

Pour chaque défaut confirmé :

1. Décris le comportement incorrect.
2. Fournis la preuve reproductible.
3. Identifie la cause.
4. Applique la correction minimale.
5. Ajoute ou adapte un test de régression pertinent.
6. Relance le test ciblé.
7. Relance les validations générales affectées.

À la fin, inspecte :

```bash
git diff --check
git status --short --branch
git diff --stat
git diff
```

Vérifie que les modifications sont limitées au périmètre autorisé et que les modifications préexistantes ont été préservées.

Ne committe pas et ne pousse pas les changements sans autorisation.

## 9. DOCUMENTATION

Mets à jour `current_step.md` pour décrire le travail réellement effectué dans le Bloc 2.

Si `CURRENT.md` doit être ajusté pour éviter une contradiction avec les résultats nouvellement observés, fais-le de manière factuelle. Ne déclare pas le Bloc 1 approuvé et ne modifie pas le périmètre des blocs suivants.

La documentation doit distinguer :

- les tests exécutés et réussis ;
- les tests exécutés et échoués ;
- les contrôles bloqués par des dépendances ou l'environnement ;
- les validations non exécutées ;
- les défauts corrigés ;
- les limitations restantes.

N'inscris jamais `PASS` sur la base d'une supposition.

## 10. FORMAT DU RAPPORT FINAL

Réponds uniquement avec ces sections :

### A. Résumé du Bloc 2
État réel, travail effectué et résultat général.

### B. Tableau des preuves

Pour chaque contrôle, indique :

| Contrôle | Commande | Code de sortie | Statut | Observation |
|---|---|---:|---|---|

Utilise uniquement des résultats réellement observés.

### C. Défauts et corrections
Liste les défauts confirmés, leurs causes, les corrections et les tests de régression.

### D. Ansible et Terraform
Pour chaque outil : versions, validations exécutées, résultats et blocages.

### E. Fichiers modifiés
Chemins exacts et justification de chaque modification.

### F. Git
Branche, HEAD, état final et confirmation concernant les modifications préexistantes. Indique si un commit a été créé ; normalement, aucun.

### G. Décision technique du Bloc 2
Choisis exactement un statut :

- `BLOC 2 — VALIDÉ TECHNIQUEMENT`
- `BLOC 2 — INCOMPLET : PREUVES MANQUANTES`
- `BLOC 2 — ÉCHEC : CORRECTIONS NÉCESSAIRES`

Ne commence pas le Bloc 3 du projet. Attends le retour du PO/QA.

**Règle finale : priorité à la fiabilité et aux preuves reproductibles, jamais à l'apparence d'un résultat parfait.**

## COMPTE RENDU D'EXÉCUTION — BLOC 2 (2026-10-14)

### État initial

- Branche: `develop`.
- HEAD: `3df0215185fec2e5f174a238bd257ec4250a7da1`, conforme au commit attendu.
- Modifications initiales: uniquement ce fichier (`current_step.md`); elles ont été préservées.
- `git diff --check`: code 2; espaces finaux dans les lignes 6 et 7 déjà présentes dans le contenu local de ce fichier.
- Aucun fichier `AGENTS.md`, `copilot-instructions.md` ou `*.instructions.md` n'a été trouvé par `rg --files`.
- Cibles Makefile disponibles: `fmt-check`, `vet`, `build`, `test`, `test-race` (ainsi que les cibles spécialisées listées dans Makefile).

### Preuves Go observées

| Contrôle | Commande | Code | Statut | Observation |
|---|---|---:|---|---|
| Packages d'artefacts | `go test -count=1 ./pkg/protocol ./internal/adapters/generation ./provider/internal/adapters/filesystem ./provider/internal/adapters/executors ./provider/internal/delivery/grpcapi ./agent/internal/adapters/providergrpc ./agent/internal/delivery/artifacts` | 0 | PASS | Tous les packages testés réussissent; `agent/internal/adapters/providergrpc` n'a pas de fichiers de test dédiés. |
| Suite complète | `go test -count=1 ./...` | 0 | PASS | Tous les packages du module réussissent. |
| Format Go | `make fmt-check` | 0 | PASS | Cible Makefile existante. |
| Analyse statique | `make vet` | 0 | PASS | Cible Makefile existante. |
| Build | `make build` | 0 | PASS | Binaire Provider et Agent construits. |
| Course Go | `go test -race -count=1 ./...` | 0 | PASS | Suite complète exécutée avec le race detector. |
| Espaces dans le diff complet | `git diff --check` | 2 | FAIL | Les espaces finaux préexistants de ce fichier sont la seule erreur observée; non modifiés afin de préserver le contenu utilisateur. |

Aucun défaut supplémentaire n'a été confirmé pendant le Bloc 2; aucun code ou test n'a été modifié pour cette passe.

### Ansible

- `ansible-playbook --version`: code 127, `zsh: command not found: ansible-playbook` — `BLOCKED`.
- `ansible-galaxy collection list`: code 127, `zsh: command not found: ansible-galaxy` — `BLOCKED`.
- Les artefacts versionnés `agent-data/lab/ansible/site.yml` et `inventory.yml` ont été inspectés. Le playbook générique ne fait qu'afficher des métadonnées via `ansible.builtin.debug`; ce n'est pas une configuration réseau.
- Les profils vendor/collections sont décrits par `internal/adapters/generation/vendor_ansible.go` et exercés par les tests Go. Aucune génération ou syntax-check Ansible n'a été exécuté dans cet environnement.

### Terraform

- `terraform version`: code 127, `zsh: command not found: terraform` — `BLOCKED`.
- Les artefacts versionnés sous `agent-data/lab/terraform` et `provider-data/lab/terraform` ont été inspectés. `main.tf` ne déclare aucune ressource; la génération est data-only.
- `terraform fmt -check`, `terraform init -backend=false` et `terraform validate`: `NOT RUN`, car l'exécutable Terraform manque. Aucun `terraform apply` exécuté.

### Périmètre restant

Bloc 2 Go local validé. Les validations Ansible/Terraform demeurent bloquées par l'absence des outils. Le contrôle global `git diff --check` reste en échec à cause du contenu utilisateur préexistant ci-dessus. Le Bloc 1 conserve son statut historique `INCOMPLET — PREUVES MANQUANTES`; le Bloc 3 n'a pas commencé. Aucun commit ni push effectué.