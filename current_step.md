From 55a458d23bb7535b7e33b27c5ed3088a7f7c7941 Mon Sep 17 00:00:00 2001
From: QA patch <qa@example.invalid>
Date: Wed, 14 Oct 2026 14:48:16 +0000
Subject: [PATCH] qa(bloc3): make preflight executable, add offline script
 test, qualify GNS3 404 finding, fix HEAD reference

---
 CURRENT.md                          |  4 +--
 Makefile                            |  5 +++-
 docs/lab/cisco-ios-gns3.md          | 13 ++++++++-
 scripts/cisco-ios-preflight.sh      |  0
 scripts/cisco-ios-preflight_test.sh | 45 +++++++++++++++++++++++++++++
 5 files changed, 63 insertions(+), 4 deletions(-)
 mode change 100644 => 100755 scripts/cisco-ios-preflight.sh
 create mode 100755 scripts/cisco-ios-preflight_test.sh

diff --git a/CURRENT.md b/CURRENT.md
index 104456e..391b87e 100644
--- a/CURRENT.md
+++ b/CURRENT.md
@@ -132,10 +132,10 @@ This later pass supersedes only the external-tool-blocked findings above; the Bl
 
 ## Bloc 3 lab status — 2026-10-14
 
-Bloc 3 was limited to a runbook and read-only preflight; no device configuration was attempted. Bloc 1 remains **INCOMPLET — PREUVES MANQUANTES**. The latest recorded Bloc 2 decision remains incomplete at its prior validation HEAD (`3df0215`); `git diff --check` passed at the current Bloc 3 HEAD (`96904f0`), but the full Bloc 2 suite was not rerun on this HEAD.
+Bloc 3 was limited to a runbook and read-only preflight; no device configuration was attempted. Bloc 1 remains **INCOMPLET — PREUVES MANQUANTES**. The latest recorded Bloc 2 decision remains incomplete at its prior validation HEAD (`3df0215`); the Bloc 3 changes were committed on top of `96904f0` (commit `bb84b29`). QA review confirmed `git diff --check 96904f0 bb84b29` exits 0, but the full Bloc 2 Go suite was not rerun by QA on `bb84b29` (Go 1.25 was unavailable in the QA environment).
 
 - Git was on `develop`, HEAD `96904f08a91967b3c7fd307bbc996bec8f0e2b16`; only `current_step.md` was modified before this pass.
-- A local `gns3server` process/listener on port 3080 was observed. The QEMU process identified was named `fortigate-1` (excluded). `GET http://127.0.0.1:3080/v2/projects` returned HTTP 404. No authorized Cisco IOS node, management IP, or independently verified SSH fingerprint was confirmed. No SSH or `/api/v1/executions` call was attempted.
+- A local `gns3server` process/listener on port 3080 was observed. The QEMU process identified was named `fortigate-1` (excluded). `GET http://127.0.0.1:3080/v2/projects` returned HTTP 404; this is **not conclusive**, because GNS3 3.x serves its API under `/v3` (see the API probe in `docs/lab/cisco-ios-gns3.md`) and `/v3/projects` was not tried. No authorized Cisco IOS node, management IP, or independently verified SSH fingerprint was confirmed. No SSH or `/api/v1/executions` call was attempted.
 - README addresses `192.168.0.2`/`.3` refer to Agent/Provider; `examples/cisco-ios-lab.yaml` labels `192.0.2.10` TEST-NET documentation. Neither is used as a router target.
 - Cases A (hostname), B (Ethernet IPv4/admin-state), and C (invalid CIDR) are `NON EXÉCUTÉ`; before/after IOS state, execution read-back, and secret-log checks are unavailable.
 - Added `docs/lab/cisco-ios-gns3.md` and `scripts/cisco-ios-preflight.sh`. The preflight requires an independently verified fingerprint and SSH public-key auth, then runs only `show version`. Syntax and invalid-input paths were checked locally; no target was contacted.
diff --git a/Makefile b/Makefile
index be02f58..5093a65 100644
--- a/Makefile
+++ b/Makefile
@@ -1,4 +1,4 @@
-.PHONY: fmt fmt-check test test-race test-provider test-agent vet build build-provider build-agent
+.PHONY: fmt fmt-check test test-race test-scripts test-provider test-agent vet build build-provider build-agent
 
 fmt:
 	gofmt -w $(shell find agent internal pkg provider -name '*.go')
@@ -12,6 +12,9 @@ test:
 test-race:
 	go test -race ./...
 
+test-scripts:
+	bash scripts/cisco-ios-preflight_test.sh
+
 test-provider:
 	go test ./provider/...
 
diff --git a/docs/lab/cisco-ios-gns3.md b/docs/lab/cisco-ios-gns3.md
index e606ee7..c7f4e1d 100644
--- a/docs/lab/cisco-ios-gns3.md
+++ b/docs/lab/cisco-ios-gns3.md
@@ -23,6 +23,17 @@ Topologie à confirmer dans GNS3 avant le test : Provider InfraFlow et un routeu
 
 Les identifiants ne sont jamais dans le YAML, la requête JSON, les commandes sauvegardées, les preuves ni le dépôt. Fournir les secrets au processus Provider depuis le gestionnaire prévu. Ne pas activer la persistance startup-config; l'exécuteur ne lance pas `write memory`.
 
+## Sonde de l'API GNS3 (lecture seule)
+
+Un `404` sur `/v2/projects` ne prouve pas qu'aucun projet n'existe : GNS3 3.x expose son API sous `/v3` (et peut exiger une authentification), alors que GNS3 2.x utilise `/v2`. Avant de conclure à l'absence de nœud Cisco, interroger les deux versions, sans rien modifier :
+
+```bash
+curl -s -o /dev/null -w '/v3/version -> %{http_code}\n' http://127.0.0.1:3080/v3/version
+curl -s -o /dev/null -w '/v2/version -> %{http_code}\n' http://127.0.0.1:3080/v2/version
+```
+
+Un `200` indique la version d'API active; `401`/`403` indique qu'une authentification est requise (ne pas coller le jeton dans le dépôt ni dans les preuves). Une fois la version identifiée, lister les projets puis les nœuds de cette version et noter le modèle de chaque nœud : seul un nœud Cisco IOS/IOSv/IOL/CSR1000v autorisé par l'opérateur peut servir de cible. Un nœud d'un autre fournisseur (par exemple `fortigate-1`) reste hors périmètre.
+
 ## Pré-vol en lecture seule
 
 Obtenir l'empreinte SHA-256 attendue depuis la console IOS ou un canal administratif indépendant. La clé retournée par `ssh-keyscan` n'est qu'une clé candidate; le script refuse de continuer si elle ne correspond pas à l'empreinte attendue. Le script n'accepte pas de mot de passe SSH : utiliser une clé déjà chargée dans `ssh-agent`.
@@ -36,7 +47,7 @@ IOS_HOSTKEY_FINGERPRINT='SHA256:<empreinte-confirmée-hors-bande>'
 bash scripts/cisco-ios-preflight.sh "$IOS_HOST" "$IOS_SSH_USER" "$IOS_HOSTKEY_FINGERPRINT"
 ```
 
-Le script récupère une clé candidate, vérifie l'empreinte, puis exécute uniquement `show version` avec authentification SSH par clé, `BatchMode=yes` et contrôle strict de clé d'hôte. Il ne modifie pas `known_hosts` et ne configure pas le routeur. Une sortie `PASS` vérifie uniquement la joignabilité SSH, l'empreinte et la détection d'une ligne de version; elle ne prouve ni une configuration ni la compatibilité de l'exécuteur.
+Le script récupère une clé candidate, vérifie l'empreinte, puis exécute uniquement `show version` avec authentification SSH par clé, `BatchMode=yes` et contrôle strict de clé d'hôte. Il ne modifie pas `known_hosts` et ne configure pas le routeur. Les chemins d'erreur d'entrée du script sont couverts hors ligne par `make test-scripts`. Une sortie `PASS` vérifie uniquement la joignabilité SSH, l'empreinte et la détection d'une ligne de version; elle ne prouve ni une configuration ni la compatibilité de l'exécuteur.
 
 Avant les requêtes, ouvrir la console IOS et enregistrer, sans données d'authentification :
 
diff --git a/scripts/cisco-ios-preflight.sh b/scripts/cisco-ios-preflight.sh
old mode 100644
new mode 100755
diff --git a/scripts/cisco-ios-preflight_test.sh b/scripts/cisco-ios-preflight_test.sh
new file mode 100755
index 0000000..18b75a9
--- /dev/null
+++ b/scripts/cisco-ios-preflight_test.sh
@@ -0,0 +1,45 @@
+#!/usr/bin/env bash
+# Offline regression test for cisco-ios-preflight.sh input validation.
+# Every case must exit 64 BEFORE any network access.
+set -uo pipefail
+
+script="$(dirname "$0")/cisco-ios-preflight.sh"
+fp='SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'
+failures=0
+
+expect_64() {
+	local name=$1
+	shift
+	bash "$script" "$@" >/dev/null 2>&1
+	local code=$?
+	if [[ $code -eq 64 ]]; then
+		printf 'PASS %s\n' "$name"
+	else
+		printf 'FAIL %s (exit %s, expected 64)\n' "$name" "$code"
+		failures=$((failures + 1))
+	fi
+}
+
+expect_64 "no arguments"
+expect_64 "too many arguments" h u "$fp" 22 extra
+expect_64 "invalid host" 'bad host' u "$fp"
+expect_64 "host with shell metachar" 'h;id' u "$fp"
+expect_64 "invalid user" h 'u;x' "$fp"
+expect_64 "fingerprint without SHA256 prefix" h u abc
+expect_64 "fingerprint with MD5 format" h u 'MD5:aa:bb'
+expect_64 "port zero" h u "$fp" 0
+expect_64 "port too large" h u "$fp" 70000
+expect_64 "port not numeric" h u "$fp" abc
+
+if bash -n "$script"; then
+	printf 'PASS bash syntax\n'
+else
+	printf 'FAIL bash syntax\n'
+	failures=$((failures + 1))
+fi
+
+if [[ $failures -ne 0 ]]; then
+	printf '%s failure(s)\n' "$failures"
+	exit 1
+fi
+printf 'All preflight script checks passed.\n'
-- 
2.43.0Tu interviens sur le dépôt existant InfraFlow, projet Go de génération et d'orchestration de configurations réseau.

CONTEXTE :
InfraFlow doit transformer une description réseau déclarative en YAML en configurations réseau spécifiques aux constructeurs, en utilisant des templates prédéfinis et un catalogue de matériels réseau.

CONTRAINTE ABSOLUE :
La soutenance du projet est demain. Il faut finaliser rapidement une tranche fonctionnelle de bout en bout. Ne reconstruis pas l'application et ne modifie pas inutilement l'architecture existante.

MISSION :
1. Examiner le dépôt, la branche active, le README, les packages Go, les fichiers YAML, les templates, les tests et la TUI.
2. Identifier les composants déjà fonctionnels et ceux qui sont incomplets.
3. Retrouver le chemin réel d'exécution entre l'entrée YAML et la génération de configuration.
4. Identifier les fonctionnalités existantes à réutiliser pour le catalogue matériel, les fiches techniques, la comparaison de templates et la génération.
5. Définir le plus petit ensemble de modifications permettant une démonstration réelle et reproductible.

LIVRABLES :
- Un état précis de l'existant.
- Une liste des problèmes bloquants classés par priorité.
- Les fichiers existants à modifier et les nouveaux fichiers réellement nécessaires.
- Un plan d'implémentation ordonné et court.
- Les tests qui prouveront le fonctionnement.

RÈGLES :
- Ne modifie aucun fichier pendant cette première étape.
- N'invente aucune fonctionnalité existante.
- Ne remplace pas une implémentation fonctionnelle par une nouvelle.
- Ne propose pas de fausses données de démonstration présentées comme réelles.
- Signale les commandes, dépendances ou composants manquants.
- Reste centré sur YAML déclaratif, catalogue matériel, templates et génération de configuration.

Termine par une recommandation claire : quel est le chemin le plus court pour obtenir une démonstration complète ?