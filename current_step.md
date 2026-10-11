# INFRAFLOW — PHASE 1 : FINALISATION DU SOCLE EXISTANT

Dépôt : `Nouments/infraflow`
Branche : `develop`

## Objectif

Continuer le développement d’InfraFlow jusqu’à obtenir une implémentation cohérente, maintenable et prête pour les prochaines fonctionnalités. Les tests complets et les builds Docker lourds seront réalisés dans une campagne finale distincte.

Consulte obligatoirement `INFRAFLOW_SPEC.md`, `ARCHITECTURE.md`, `README.md`, `CURRENT.md` et le code réel avant de modifier quoi que ce soit.

## Règles

- Ne crée aucun fichier YAML d’exemple ou de démonstration supplémentaire.
- Ne crée aucune donnée fictive pour simuler le fonctionnement.
- Ne remplace pas une fonctionnalité réelle par un mock.
- Ne réécris pas les composants qui fonctionnent déjà sans raison démontrée.
- Ne mets aucun secret, token, certificat privé ou mot de passe dans Git.
- Ne reconstruis pas les images Docker lourdes pendant cette phase.
- Préserve les modifications préexistantes et vérifie `git status`.
- Ne pousse aucun changement sans autorisation.
- N’annonce pas une fonctionnalité terminée sans avoir identifié son implémentation réelle et ses limites.

## Travail

### 1. État du dépôt

Inspecte les composants existants du Provider, de l’Agent, du scheduler, de la génération des artefacts, des APIs, du TUI, du dashboard et des services bootstrap.

Repère les fonctionnalités annoncées dans la documentation mais absentes, partielles, déconnectées ou incorrectement intégrées.

### 2. Configuration et démarrage

Vérifie les contrats de configuration Go, les variables d’environnement, les chemins des fichiers, les entrypoints et les Dockerfiles.

Corrige les défauts évidents directement dans les fichiers existants. Prépare une configuration cohérente entre code source et conteneurs, sans ajouter de configurations de démonstration.

Ne prétends pas résoudre les erreurs observées dans GNS3 tant que leur cause n’est pas confirmée.

### 3. Cohérence de l’architecture

Vérifie que :
- le Provider valide et planifie sans prétendre exécuter ;
- les jobs possèdent des états cohérents et persistants ;
- le scheduler et les contrats applicatifs sont prêts à être reliés au futur moteur d’exécution ;
- l’Agent ne déclare pas un déploiement réussi après une simple génération ou validation ;
- les résultats, journaux et erreurs disposent de chemins de remontée cohérents ;
- les capacités non implémentées restent explicitement bloquées ;
- les API, le TUI et le dashboard reflètent les états réellement enregistrés.

Corrige les défauts d’intégration découverts dans le périmètre existant. Ne commence pas encore les adaptateurs spécifiques aux constructeurs.

### 4. Documentation

Remets `CURRENT.md` en document de suivi réel : état actuel, tâches terminées avec preuves disponibles, tâches restantes et prochaine étape.

La documentation ne doit pas être remplacée par un prompt. Mets à jour `README.md`, `ARCHITECTURE.md` ou `INFRAFLOW_SPEC.md` uniquement lorsque le code réellement modifié le justifie.

### 5. Tests légers disponibles

Sans lancer les builds Docker lourds, exécute les tests unitaires et vérifications locales ciblées possibles.

Si des tests échouent, corrige les défauts dans le périmètre de cette phase. N’élargis pas artificiellement les tests et n’en supprime aucun pour obtenir un résultat vert.

## Livrables obligatoires

À la fin, indique :
1. les défauts confirmés ;
2. les fichiers réellement modifiés ;
3. les corrections réalisées ;
4. les tests effectivement exécutés et leurs résultats ;
5. les fonctionnalités encore absentes ;
6. la prochaine tâche d’implémentation à traiter.

Ne commence pas la phase suivante dans la même intervention. Ne pousse rien et ne prétends pas que le projet est entièrement terminé.