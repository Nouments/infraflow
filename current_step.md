# CURRENT.md — InfraFlow Code Review & Remediation Tasks

## 1. Mission

Effectuer une correction complète et vérifiable des problèmes identifiés lors de la revue du dépôt `Nouments/infraflow`.

Le travail doit porter sur le code existant, sans reconstruire inutilement l'architecture ni remplacer des fonctionnalités déjà fonctionnelles.

**Priorités :**

1. Corriger les références documentaires cassées.
2. Terminer l'intégration des logs dans le dashboard Web.
3. Implémenter la capture et la transmission des sorties réelles des processus.
4. Ajouter une vue Logs dans le TUI.
5. Ajouter des tests unitaires et d'intégration pertinents.
6. Mettre à jour `README.md` et les documents de référence avec des preuves vérifiables.

Ne pas considérer une fonctionnalité comme terminée uniquement parce que ses fichiers, ses interfaces ou ses endpoints existent.

## 2. Règles obligatoires

Respecter les principes suivants pendant toute l'implémentation :

* `MOCK != REAL`
* `TODO != DONE`
* `GENERATED != EXECUTED`
* `EXECUTED != VERIFIED`
* `VERIFIED != LAB-TESTED`
* `INFERRED != OBSERVED`

Interdictions :

* Ne pas inventer de données, de résultats de tests ou de captures d'écran.
* Ne pas afficher de logs fictifs dans le dashboard pour simuler une fonctionnalité opérationnelle.
* Ne pas déclarer qu'une configuration Cisco, FortiGate, MikroTik ou Proxmox a été appliquée sans preuve d'exécution.
* Ne pas déclarer qu'un équipement est configuré ou opérationnel sur la seule base d'un fichier généré.
* Ne pas supprimer des fonctionnalités existantes pour simplifier l'implémentation.
* Ne pas remplacer les tests réels par des mocks sans conserver les tests adaptés au comportement réel.
* Ne pas introduire de dépendances lourdes sans justification.
* Ne pas modifier inutilement l'architecture, les contrats API ou les formats de données existants.

Avant toute modification, examiner le dépôt et identifier les composants, contrats, tests et conventions déjà présents. Adapter les changements à l'architecture existante.

---

## 3. Tâche P0 — Corriger la documentation et les références

### Objectif

Éliminer les références documentaires cassées et maintenir une documentation cohérente avec le code réel.

### Travaux

1. Examiner toutes les références à `INFRAFLOW_SPEC.md`.
2. Vérifier si le fichier a été supprimé, renommé ou remplacé.
3. Si sa suppression n'est pas intentionnelle, restaurer une version cohérente avec l'architecture et les fonctionnalités réellement implémentées.
4. Si la spécification a été remplacée, corriger toutes les références dans `README.md`, les fichiers Markdown et les instructions des agents.
5. Vérifier la suppression de `.github/agents/infraflow-engineer.agent.md` :

   * déterminer si elle est intentionnelle ;
   * restaurer le fichier uniquement si son contenu est encore nécessaire ;
   * sinon, documenter le remplacement ou la nouvelle organisation des instructions.

### Critères d'acceptation

* Aucun lien interne important ne pointe vers un fichier inexistant.
* Le README décrit les fonctionnalités existantes, en cours et prévues séparément.
* La spécification ne prétend pas que les fonctionnalités futures sont déjà opérationnelles.
* Les suppressions de fichiers sont expliquées ou justifiées par l'historique du dépôt.

---

## 4. Tâche P1 — Terminer l'observabilité dans le dashboard Web

### Constat à vérifier

Le backend possède des endpoints de logs, mais le dashboard ne semble pas encore fournir une vue complète permettant de consulter ces données.

Vérifier le code actuel avant de confirmer ce constat.

### Endpoints concernés

Examiner notamment les routes existantes :

* `GET /api/v1/logs`
* `GET /api/v1/logs/stream`
* `GET /api/v1/logs/runs/{id}`

Respecter les méthodes, paramètres, formats et mécanismes d'authentification déjà définis dans le code. Ne pas supposer que ces endpoints possèdent des fonctionnalités qui n'ont pas été vérifiées.

### Travaux

Ajouter une vue Web « Logs techniques » intégrée au dashboard existant.

Elle doit permettre, selon les capacités réellement disponibles dans l'API :

* consulter les logs persistés ;
* afficher les nouveaux événements en temps réel ;
* filtrer par niveau de gravité ;
* rechercher par texte ;
* filtrer par site et agent ;
* filtrer par exécution (`run_id`), job ou tâche lorsque ces identifiants existent ;
* afficher la date, la source, le niveau, le message et les métadonnées utiles ;
* consulter les détails d'un événement ;
* distinguer les événements d'exécution, les erreurs et les événements de synchronisation ;
* gérer proprement les erreurs réseau ;
* afficher un état de connexion explicite ;
* permettre la reconnexion au flux SSE après une interruption ;
* éviter l'accumulation illimitée d'événements dans le navigateur.

### SSE et cohérence des données

Si le flux SSE existe déjà :

* examiner le format des événements ;
* utiliser les identifiants d'événements lorsqu'ils sont disponibles ;
* reprendre la réception après une interruption sans prétendre garantir une reprise si le backend ne la permet pas ;
* éviter les doublons lors de la fusion des événements historiques et temps réel ;
* gérer les déconnexions et les erreurs sans bloquer le reste du dashboard.

Ne pas créer un second système de logs indépendant pour le frontend.

### Critères d'acceptation

* La vue est accessible depuis la navigation existante.
* Elle utilise les véritables endpoints du backend.
* Les logs affichés proviennent des données réellement reçues ou persistées.
* Les filtres ont un effet réel.
* Les états de chargement, d'erreur et d'absence de données sont gérés.
* Les permissions existantes sont respectées.
* Les erreurs d'autorisation ne sont pas masquées comme de simples listes vides.
* Les tests couvrent les appels API et les comportements essentiels de l'interface, selon les outils de test déjà présents.

---

## 5. Tâche P1 — Capturer les sorties réelles des processus

### Constat à vérifier

Examiner `agent/internal/application/toolrunner/runner.go` et les composants qui lancent les commandes.

Si stdout et stderr sont actuellement collectés dans un buffer commun puis transmis à la fin du processus, compléter l'implémentation pour fournir une observabilité en direct.

### Objectifs

Pour chaque exécution prise en charge, permettre la capture des sorties réelles du processus :

* stdout ;
* stderr ;
* code de sortie ;
* heure de démarrage ;
* heure de fin ;
* durée ;
* statut final ;
* erreur de lancement ou d'exécution ;
* identifiants du site, de l'agent, du run, du job et de la tâche, lorsqu'ils sont disponibles.

### Travaux

1. Lire les sorties du processus pendant son exécution.
2. Distinguer stdout et stderr dans les événements.
3. Transmettre les événements au système de logs existant.
4. Conserver une limite de taille pour les buffers et les événements.
5. Prévenir les blocages lorsque stdout et stderr produisent simultanément des données.
6. Gérer les sorties volumineuses sans consommation mémoire non bornée.
7. Gérer les timeouts et l'annulation lorsque le système d'exécution les prend en charge.
8. Enregistrer les erreurs de lancement, les erreurs de lecture et le code de sortie.
9. Associer les événements au bon contexte d'exécution.
10. Éviter de journaliser les secrets, mots de passe, jetons, clés privées et autres données sensibles.
11. Préserver les mécanismes existants de persistance locale et de synchronisation différée.
12. Vérifier le comportement lorsque le provider central est indisponible.

### Sécurité

* Appliquer la redaction avant la persistance et la transmission des données sensibles.
* Ne jamais journaliser intentionnellement les secrets d'authentification.
* Ne pas désactiver TLS ou les contrôles d'identité pour faciliter les tests.
* Limiter la taille des messages et contrôler les erreurs de transmission.
* Ne pas transmettre de commandes arbitraires provenant de logs ou d'événements non fiables.

### Critères d'acceptation

Lancer un processus de test contrôlé qui écrit plusieurs lignes sur stdout et stderr, produit éventuellement une erreur et se termine avec un code connu.

Vérifier que :

* les lignes sont observables avant la fin du processus ;
* stdout et stderr restent distinguables ;
* les événements ont les bons identifiants ;
* le statut final et le code de sortie sont exacts ;
* un processus en erreur est correctement signalé ;
* les limites mémoire et de taille sont appliquées ;
* les secrets utilisés dans les tests ne sont pas présents dans les logs ;
* la perte de connexion centrale ne provoque pas la perte silencieuse des événements lorsque l'outbox est censée les conserver.

Ne pas prétendre que les commandes réseau sont exécutées si seuls des processus de test contrôlés ont été validés.

---

## 6. Tâche P2 — Ajouter une vue Logs au TUI

### Objectif

Permettre de consulter les mêmes événements techniques dans le TUI de l'agent ou dans l'interface TUI appropriée déjà définie par l'architecture.

### Travaux

Avant l'implémentation, identifier le TUI existant et déterminer s'il s'agit d'un TUI local à l'agent, d'un TUI opérateur ou des deux.

Ajouter, en cohérence avec l'organisation existante :

* une vue des événements récents ;
* la consultation des erreurs ;
* des filtres par niveau ;
* une recherche textuelle si adaptée au TUI ;
* le détail d'un événement ;
* l'identification du site, de l'agent et de l'exécution ;
* l'indication de la disponibilité de la source de logs ;
* la gestion de la déconnexion et de la synchronisation différée.

Réutiliser le système de données et les structures d'événements existants. Ne pas créer une seconde source de vérité.

Si le TUI est local, permettre la consultation des logs locaux même sans accès au provider central, conformément aux capacités existantes de stockage local.

### Critères d'acceptation

* La vue s'intègre à la navigation existante.
* Elle affiche les événements réels.
* Les filtres et raccourcis fonctionnent réellement.
* L'interface reste utilisable avec un grand nombre d'événements.
* L'indisponibilité du réseau est signalée clairement.
* Les tests couvrent la logique de filtrage et les fonctions critiques.

---

## 7. Tâche P1 — Tests de bout en bout et preuves

### Objectif

Vérifier le chemin réel des événements à travers le système.

### Parcours à tester

`Processus contrôlé → Capture agent → Événements structurés → Outbox locale si nécessaire → Transmission → Provider → Stockage central → API → Dashboard`

Tester également le parcours de consultation local dans le TUI.

### Tests à ajouter

Selon les frameworks déjà présents :

* tests unitaires du logger ;
* tests de redaction ;
* tests de rotation et de limites de taille ;
* tests de persistance ;
* tests de déduplication ;
* tests de l'outbox ;
* tests de validation de l'identité agent/site ;
* tests de l'ingestion gRPC et HTTP ;
* tests du flux SSE ;
* tests de reconnexion et d'erreurs réseau ;
* tests des filtres de l'API ;
* tests du lancement et de la capture stdout/stderr ;
* tests des timeouts et annulations ;
* tests de la vue Logs du dashboard ;
* tests de la vue Logs du TUI ;
* tests d'intégration couvrant le parcours agent-provider-API.

Ne pas remplacer les tests de comportement réel par des assertions qui vérifient uniquement que des fonctions ont été appelées.

### Vérifications réseau distinctes

Les tests d'observabilité ne prouvent pas que l'automatisation réseau fonctionne.

Distinguer explicitement :

1. le test du logger ;
2. le test de lancement d'un processus contrôlé ;
3. le test d'intégration agent-provider ;
4. le test d'une commande d'administration réelle ;
5. le test dans un laboratoire réseau ;
6. la vérification post-déploiement de l'état effectif d'un équipement.

N'affirmer les niveaux 4 à 6 que si les preuves correspondantes existent.

---

## 8. Tâche P1 — Mettre à jour le README.md

Mettre à jour le README après l'implémentation et l'exécution des tests.

Le README doit présenter :

### Architecture

* les composants réellement présents ;
* le rôle de l'agent ;
* le rôle du provider ;
* le stockage local et central ;
* les mécanismes de transport ;
* le fonctionnement hors ligne, uniquement dans les limites réellement implémentées ;
* les interfaces Web et TUI réellement disponibles.

### Observabilité

Documenter :

* les formats d'événements ;
* les chemins de logs locaux réels ;
* les routes API existantes ;
* l'authentification et les permissions ;
* le flux SSE ;
* la recherche et les filtres réellement supportés ;
* la redaction ;
* la rotation et les limites ;
* le fonctionnement de l'outbox ;
* le comportement en cas de perte de connexion ;
* les limitations connues.

Ne pas inventer des chemins de fichiers : les récupérer depuis le code et les valeurs de configuration réelles.

### Installation et démonstration

Fournir des commandes copiables et vérifiées pour :

* compiler le projet ;
* lancer les tests ;
* démarrer les composants nécessaires ;
* lancer une démonstration locale ;
* générer des événements avec un processus de test contrôlé ;
* consulter les logs dans l'API ;
* ouvrir la vue Logs dans le dashboard et le TUI, si ces vues sont effectivement implémentées.

Préciser les prérequis et les variables d'environnement nécessaires. Ne pas exposer de secrets réels.

### État des fonctionnalités

Créer ou mettre à jour une matrice d'état :

| Fonctionnalité          | État                  | Preuve                        |
| ----------------------- | --------------------- | ----------------------------- |
| Logging structuré       | À vérifier            | Tests et fichiers concernés   |
| Persistance locale      | À vérifier            | Tests de persistance          |
| Outbox hors ligne       | À vérifier            | Test déconnecté/reconnexion   |
| Ingestion centrale      | À vérifier            | Test d'intégration            |
| API de logs             | À vérifier            | Tests HTTP/gRPC               |
| Flux SSE                | À vérifier            | Test de streaming             |
| Dashboard Logs          | À vérifier            | Test ou démonstration réelle  |
| TUI Logs                | À vérifier            | Test ou démonstration réelle  |
| Capture stdout/stderr   | À vérifier            | Processus contrôlé            |
| Déploiement réseau réel | À vérifier séparément | Preuve issue d'un laboratoire |

Remplacer « À vérifier » uniquement après avoir obtenu les preuves. Utiliser des états explicites tels que `IMPLEMENTED`, `TESTED`, `LAB-VERIFIED`, `PARTIAL`, `PLANNED` ou `BLOCKED` si ces états sont définis de manière cohérente dans la documentation.

---

## 9. Procédure de travail des agents

Travailler dans cet ordre :

1. Lire `CURRENT.md`, `README.md`, la spécification et les instructions des agents disponibles.
2. Examiner le statut Git, les branches, les commits et les changements non validés.
3. Identifier les implémentations déjà présentes et les problèmes réellement confirmés.
4. Établir une courte liste des fichiers à modifier et des risques.
5. Implémenter les corrections minimales cohérentes avec l'architecture.
6. Ajouter les tests correspondants.
7. Exécuter les tests réellement disponibles.
8. Examiner les différences Git et rechercher les régressions.
9. Mettre à jour `README.md` avec les résultats exacts.
10. Fournir un rapport final fondé sur les preuves.

Ne pas écraser les changements de travail existants. Ne pas faire de commit ou de push sans autorisation explicite.

Si une tâche dépend d'une fonctionnalité absente, d'un équipement indisponible ou d'un environnement de test non disponible, documenter le blocage et terminer les travaux indépendants.

---

## 10. Rapport final obligatoire

À la fin, fournir :

### A. Modifications réalisées

Pour chaque modification :

* fichiers concernés ;
* comportement ajouté ou corrigé ;
* justification technique.

### B. Tests réellement exécutés

Pour chaque test :

* commande exacte ;
* résultat réel ;
* succès ou échec ;
* éventuels logs ou limites observés.

Ne jamais présenter un test non exécuté comme réussi.

### C. Fonctionnalités non terminées

Lister :

* les fonctionnalités partielles ;
* les tests impossibles à exécuter ;
* les limitations connues ;
* les dépendances externes ;
* les vérifications nécessitant un véritable laboratoire réseau.

### D. Documentation

Indiquer :

* les références corrigées ;
* les sections ajoutées au README ;
* les instructions vérifiées ;
* les documents restaurés ou remplacés.

### E. État final

Séparer clairement :

* implémenté ;
* testé automatiquement ;
* testé en intégration ;
* vérifié dans un laboratoire ;
* non vérifié ;
* bloqué.

## Critère final de réussite

La tâche est terminée uniquement lorsque les changements sont présents dans le code, que les tests pertinents ont été exécutés et que le README reflète fidèlement les capacités réellement démontrées.

**La présence d'un endpoint, d'une page ou d'une fonction ne constitue pas à elle seule une preuve de fonctionnement.**
