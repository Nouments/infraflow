# INFRAFLOW — TASK 3.1 : DURCISSEMENT DU TRANSPORT SSH CISCO IOS

**Repository :** `Nouments/infraflow`  
**Branche :** `develop`  
**Commit de départ :** `b034adca9dca8e644a8b6ace11805017b315d872`
**Commit attendu :** `fix(provider): harden Cisco IOS SSH execution lifecycle`

## OBJECTIF

Corriger les problèmes de cycle de vie SSH, de mémoire, d’annulation, de répétition après échec et de sécurité documentaire détectés lors de la revue de `cisco.ios.configure`.

Ne réécris pas l’exécuteur. Ne développe pas NAT, OSPF, ACL, VLAN ou d’autres constructeurs dans cette tâche.

## 1. Cycle de vie des goroutines et des sessions SSH

Examine `provider/internal/adapters/executors/cisco_ios_transport.go`.

Corrige les points suivants :

- Une lecture bloquée ne doit pas laisser une goroutine en attente après expiration du délai ou annulation.
- La fermeture normale d’une session doit libérer le goroutine surveillant le contexte.
- Le mécanisme de fermeture doit être idempotent et sûr en présence de concurrence.
- Toute expiration de délai, erreur de lecture ou annulation doit permettre de fermer proprement la connexion et les ressources associées.
- Une session fermée ne doit plus accepter de nouvelles commandes.
- Les accès concurrents aux flux de lecture et d’écriture doivent être sûrs.
- Évite de lancer une nouvelle goroutine de lecture à chaque itération si un lecteur persistant avec canal de données permet une gestion plus fiable.

N’introduis ni polling inutile, ni attente arbitraire, ni fuite de goroutines.

Ajoute des tests déterministes couvrant fermeture normale, annulation, délai dépassé, lecture interrompue, appels concurrents à `Close` et tentative d’utilisation après fermeture.

## 2. Limitation de la sortie SSH

La sortie des commandes IOS doit avoir une limite configurable ou une constante documentée, adaptée à la première version.

- Ne conserve jamais une sortie réseau sans limite en mémoire.
- En cas de dépassement, retourne une erreur explicite et non sensible.
- Ferme proprement la session et interromps la lecture.
- N’accepte jamais une sortie tronquée comme une configuration vérifiée.
- Teste une sortie dépassant la limite et vérifie qu’aucun succès n’est déclaré.

Les limites doivent être compatibles avec les commandes réellement utilisées par l’exécuteur. Documente-les.

## 3. Prompt IOS et détection des erreurs

Dans le transport, vérifie que la fin d’une commande est déterminée par le prompt final attendu, et non par n’importe quelle ligne ressemblant à un prompt au milieu de la sortie.

- Gère les retours à la ligne, les séquences ANSI et les espaces terminaux.
- Préserve les erreurs IOS explicites.
- N’interprète pas un timeout comme une réponse valide.
- Évite les boucles de lecture infinies.
- Ajoute des tests où la sortie contient une ligne ressemblant à un prompt avant la véritable fin de commande.
- Ajoute des tests sur les sorties vides, les bannières, les erreurs IOS et les connexions fermées.

N’élargis pas excessivement la regex des prompts pour faire passer les tests.

## 4. Rejouabilité des tâches de configuration

Examine `CiscoIOSConfigureExecutor.Retryable` et le contrat du scheduler.

Une configuration peut modifier plusieurs lignes avant qu’une erreur de transport survienne. Le scheduler ne doit pas relancer automatiquement une tâche de configuration après une application partielle ou incertaine.

Implémente une politique prudente :

- Ne rends pas rejouable automatiquement une configuration dont l’application a commencé et dont le résultat est incertain.
- Si le contrat actuel `Retryable(err)` ne permet pas de connaître la phase d’exécution, modifie l’intégration minimale nécessaire ou désactive le retry automatique pour `cisco.ios.configure`.
- Préserve les retries des tâches pour lesquelles ils sont réellement sûrs, notamment les cas existants de `artifact.verify`.
- Une erreur de connexion avant toute mutation peut être distinguée d’une erreur après le début des modifications, mais uniquement si cette distinction est réellement conservée par l’architecture.
- Si une mutation partielle a pu se produire, retourne un résultat d’échec qui indique que l’état du routeur doit être relu avant toute nouvelle tentative.
- N’annonce jamais un rollback si aucune restauration réelle et vérifiée n’a été effectuée.

Ajoute des tests prouvant qu’un timeout après la première commande de configuration ne déclenche pas une nouvelle application automatique non contrôlée.

## 5. Sécurité de la procédure SSH

Mets à jour `docs/cisco-ios-executor.md` :

- Ne recommande pas d’ajouter aveuglément le résultat de `ssh-keyscan` au fichier de confiance.
- Explique comment obtenir l’empreinte du routeur par un canal indépendant et la comparer avant approbation.
- Précise que toute clé inconnue ou modifiée doit faire échouer la connexion.
- Remplace les mots de passe en clair par des placeholders non utilisables, par exemple `<MOT_DE_PASSE_DE_TEST>`.
- N’inscris jamais un secret réel dans le YAML, le JSON d’exécution, les commandes enregistrées ou les logs.
- Explique comment retirer les variables d’environnement de test après la validation.

Ne désactive jamais la vérification de clé d’hôte.

## 6. Inventaire et documentation

Vérifie `ArtifactInventoryDeviceResolver` et la manière dont `CURRENT.md` présente l’autorisation des cibles.

- Le site et l’équipement doivent correspondre à une cible Cisco autorisée.
- Les données de l’inventaire doivent être validées avant connexion.
- Vérifie les comportements en cas d’inventaire absent, malformé, incohérent ou d’adresse de gestion invalide.
- Ne prétends pas que le fichier est vérifié cryptographiquement si ce n’est pas le cas.
- Corrige les liens vers la procédure de validation GNS3 : soit crée réellement le fichier référencé, soit remplace les liens par le chemin existant `docs/cisco-ios-executor.md`.
- Conserve clairement le statut `NON EXÉCUTÉ` pour le test GNS3 tant qu’il n’a pas réellement été effectué.

Ne transforme pas cette tâche en refonte de l’inventaire complet.

## 7. Tests obligatoires

Ajoute ou adapte des tests couvrant :

1. Fermeture et annulation SSH sans fuite de goroutines.
2. Timeout avec libération des ressources.
3. Sortie dépassant la limite mémoire.
4. Détection fiable du prompt final.
5. Détection des erreurs IOS.
6. Échec de vérification sans succès partiel.
7. Absence de retry automatique après une configuration partiellement appliquée.
8. Préservation des retries sûrs de `artifact.verify`.
9. Refus d’une cible non autorisée.
10. Absence de secrets dans les résultats et les logs.

Utilise des transports injectables pour les tests déterministes. Ne prétends pas que ces tests remplacent un test IOS réel.

## 8. Validation à exécuter

Depuis la racine du dépôt, adapte les commandes au Makefile réel puis exécute :

```bash
gofmt -w <fichiers-go-modifiés>
make fmt-check
go test -count=1 ./provider/internal/adapters/executors
go test -count=1 ./provider/internal/application/...
go test -count=1 ./...
make vet
make build
git diff --check
git status --short
git diff --stat
```

Si un chemin de package n’existe pas, identifie les packages réels avant de lancer les tests. N’ignore pas un échec et ne prétends pas avoir exécuté une commande qui ne l’a pas été.

## 9. Commit et rapport

Fais un commit dédié sur `develop` :

`fix(provider): harden Cisco IOS SSH execution lifecycle`

Dans le compte rendu, donne :

- le SHA du commit ;
- les fichiers modifiés et les corrections apportées ;
- la politique exacte de retry retenue ;
- la limite de sortie SSH retenue ;
- les tests réellement exécutés avec leurs résultats ;
- les limites restantes ;
- le statut réel de validation GNS3.

Ne mélange pas cette tâche avec le dashboard, le TUI, Docker ou de nouveaux adaptateurs.

**Critère d’acceptation :** aucune fuite connue de goroutines, aucune sortie SSH non bornée, aucune réussite à partir d’une réponse incomplète et aucune nouvelle tentative automatique dangereuse après une configuration dont le résultat est incertain. La validation GNS3 doit rester explicitement non exécutée tant qu’elle ne l’est pas réellement.