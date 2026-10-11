# INFRAFLOW — PHASE 3 : INTÉGRATION COMPLÈTE DES EXÉCUTIONS DANS LE DASHBOARD WEB ET LE TUI GO

**Repository :** `Nouments/infraflow`  
**Branche :** `develop`  
**Mode de travail :** implémentation réelle, tests locaux légers, aucune reconstruction Docker.

## 1. Objectif et périmètre strict

Le moteur d’exécution, la persistance et les routes API des exécutions existent déjà dans le projet.

Cette tâche consiste exclusivement à intégrer ces fonctionnalités dans :

1. Le dashboard web existant.
2. Le TUI Linux existant, développé en Go avec Lip Gloss et les bibliothèques de l’écosystème Charmbracelet.

Il ne faut pas réimplémenter le moteur d’exécution, créer un second scheduler, inventer des données ou commencer les adaptateurs Cisco, MikroTik, FortiGate et Proxmox.

Avant toute modification, examine le code réel et les dépendances présentes dans `go.mod` et `go.sum`. Lis `CURRENT.md`, `INFRAFLOW_SPEC.md`, `ARCHITECTURE.md`, `README.md`, les routes `/api/v1/executions`, le client HTTP du TUI, les tests existants et les composants du dashboard.

Ne suppose pas que les fonctionnalités sont absentes avant d’avoir vérifié leur implémentation actuelle.

## 2. TUI : exigences techniques obligatoires

Le TUI InfraFlow est une application terminal interactive écrite en **Go**, utilisant **Lip Gloss** pour le style et l’écosystème **Charmbracelet** pour les composants interactifs.

Il doit rester une application TUI native, légère, adaptée à Linux et utilisable à travers SSH. Il ne doit pas devenir une interface CLI rudimentaire composée uniquement de `fmt.Printf`, de menus numériques ou de saisies ligne par ligne.

### 2.1 Respect de l’architecture existante

- Conserve le langage Go et la structure actuelle des packages.
- Conserve Lip Gloss et les bibliothèques Charmbracelet déjà installées.
- Réutilise Bubble Tea si le projet l’utilise déjà pour la boucle interactive, les messages, les commandes et les mises à jour d’état.
- Si le TUI actuel n’utilise pas encore Bubble Tea, vérifie l’architecture existante et justifie toute évolution nécessaire avant d’introduire cette dépendance.
- N’ajoute pas une seconde architecture de rendu ni des bibliothèques redondantes.
- Sépare proprement les appels HTTP, les modèles de données, l’état de l’interface, les interactions et le rendu.
- Conserve l’authentification et la gestion de session actuelles.
- Respecte les conventions de code et de tests existantes.

### 2.2 Expérience interactive attendue

Intègre une section **Executions** dans la navigation actuelle du TUI.

Elle doit offrir :

- Une liste paginée des exécutions réelles provenant du Provider.
- Une sélection interactive d’une exécution.
- Une vue détaillée avec ses tâches, leurs états, leurs tentatives, leurs résultats et leurs erreurs.
- L’affichage des dates et durées réellement disponibles.
- Des indicateurs visuels distincts pour les états `queued`, `running`, `retrying`, `succeeded`, `failed`, `cancelled` et `timed_out`, lorsque ces états sont présents dans les données.
- Un rafraîchissement manuel.
- Un rafraîchissement automatique limité pour les exécutions actives, si compatible avec l’architecture actuelle.
- Une navigation claire entre la liste, le détail et l’écran précédent.
- Des raccourcis clavier cohérents avec les autres vues et affichés dans une barre d’aide.
- Des états d’interface propres pour le chargement, les listes vides, les erreurs API, la perte de connexion et les sessions expirées.
- Une demande d’annulation avec confirmation interactive pour les exécutions non terminales.

L’utilisation de Lip Gloss doit assurer une présentation cohérente : titres, panneaux, tableaux ou listes, statuts colorés, erreurs visibles et aide contextuelle. Respecte les contraintes de largeur du terminal, les retours à la ligne et les terminaux SSH sans couleurs.

Si Bubble Tea est déjà utilisé, les interactions doivent suivre son modèle `Model`, `Init`, `Update` et `View`, selon la version installée. Les appels réseau potentiellement bloquants ne doivent pas figer l’interface ; utilise les commandes asynchrones appropriées.

Ne réécris pas tout le TUI. Étends la structure existante avec le minimum de modifications architecturales nécessaires.

### 2.3 Commandes et interactions

Le parcours doit permettre de :

1. Ouvrir la vue des exécutions.
2. Parcourir la liste et changer de page.
3. Sélectionner une exécution.
4. Examiner son état général et le détail de ses tâches.
5. Consulter les erreurs, les tentatives et les résultats.
6. Demander une annulation et confirmer ou abandonner cette demande.
7. Voir la réponse réelle du Provider, puis actualiser l’état.
8. Revenir à la liste et rafraîchir les données.

Les raccourcis exacts doivent suivre les conventions existantes. Évite les collisions avec les raccourcis déjà utilisés.

## 3. Dashboard web

Étends le dashboard actuel sans reconstruire son design global.

Ajoute une vue **Executions** intégrée à la navigation existante et cohérente avec les styles, composants et mécanismes d’authentification actuels.

Elle doit afficher exclusivement les données renvoyées par l’API :

- Identifiant, état et dates de l’exécution.
- Tâches et types de tâches.
- Tentatives, résultats, erreurs et durées disponibles.
- Historique des événements de l’exécution, si présent dans le contrat retourné.
- Filtres par état et type.
- Pagination utilisant les paramètres réels de l’API.
- Détail d’une exécution.
- Action d’annulation avec confirmation, contrôle de l’état et retour explicite de la réponse du serveur.

Si la progression globale n’est pas fournie par le backend, n’invente pas un pourcentage. Tu peux afficher un décompte calculé à partir des tâches et de leurs états réels, en indiquant clairement qu’il s’agit d’un décompte et non d’une mesure de progression garantie.

N’ajoute pas de fausses données pour remplir le dashboard.

## 4. API et contrats

Réutilise les routes existantes :

- `GET /api/v1/executions`
- `POST /api/v1/executions`
- `GET /api/v1/executions/{id}`
- `POST /api/v1/executions/{id}/cancel`

Vérifie les contrats dans le code source avant de les intégrer. Respecte les paramètres de pagination, les filtres et les champs JSON réellement disponibles.

Ne crée pas de routes supplémentaires si les routes actuelles suffisent.

Si un changement backend s’avère indispensable, explique pourquoi, limite sa portée et ajoute les tests correspondants. Ne modifie pas le modèle d’exécution simplement pour simplifier le rendu.

Le TUI et le dashboard doivent utiliser le Provider comme source de vérité. Ils ne doivent pas modifier localement un état en prétendant que l’opération a réussi.

## 5. Sécurité et autorisations

- Réutilise l’authentification existante.
- Respecte les rôles et permissions imposés par le Provider.
- Ne contourne jamais le RBAC côté interface.
- Ne stocke pas de nouveau jeton dans le navigateur.
- Ne révèle pas de secrets dans les paramètres, résultats, erreurs ou journaux.
- Ne présente pas une annulation demandée comme une annulation achevée.
- Gère les erreurs HTTP, les refus d’accès et les sessions expirées.

Les autorisations doivent être vérifiées côté serveur, même si les actions interdites sont masquées dans l’interface.

## 6. Tests à ajouter

### Go / TUI

Ajoute des tests adaptés aux packages existants pour vérifier :

- Le décodage des réponses API.
- Les listes vides, la pagination et les erreurs réseau.
- L’affichage des différents états.
- La navigation liste/détail/retour.
- La confirmation et le refus d’une annulation.
- La mise à jour après la réponse du Provider.
- Les sessions expirées et les accès refusés.
- Le comportement de l’interface lorsqu’une requête échoue ou arrive en retard.

Utilise les mécanismes de test déjà présents. Si Bubble Tea est utilisé, teste les transitions de modèle et les commandes de manière déterministe sans exiger un terminal interactif réel.

### Dashboard web

Ajoute ou complète les tests JavaScript déjà utilisés par le projet :

- Rendu de la liste et des détails.
- Filtres et pagination.
- États de chargement, liste vide et erreur.
- Confirmation d’annulation.
- Gestion des réponses HTTP non réussies.
- Absence de faux état de succès.
- Absence de données fictives dans les vues d’exécution.

Exécute les tests ciblés, les tests Go concernés et les vérifications légères disponibles. Corrige les régressions introduites par cette tâche.

Ne reconstruis pas les images Docker et ne prétends pas avoir validé le fonctionnement sur GNS3 ou sur des équipements réels.

## 7. Documentation et suivi

Mets à jour `CURRENT.md`, `INFRAFLOW_SPEC.md`, `ARCHITECTURE.md` et `README.md` uniquement pour refléter ce qui est effectivement livré.

Documente les interactions du TUI, ses raccourcis, la navigation et la nouvelle vue web.

Distingue les tests exécutés des tests restant à effectuer. Ne marque aucune fonctionnalité comme terminée sans implémentation et vérification correspondantes.

Ne crée pas de fichiers YAML d’exemple supplémentaires, de données de démonstration ou de mocks utilisés comme données de production. Les mocks sont autorisés uniquement dans les tests automatisés lorsque nécessaire.

Ne pousse pas de modifications sans autorisation.

## 8. Hors périmètre explicite

Ne pas réaliser dans cette tâche :

- Les adaptateurs Cisco, MikroTik, FortiGate ou Proxmox.
- L’application de configurations sur de vrais équipements.
- La reconstruction des images Docker.
- Une refonte générale du dashboard.
- Une réécriture complète du TUI.
- Un nouveau moteur d’exécution ou un second scheduler.
- Une nouvelle API de jobs si les routes existantes suffisent.
- Des données fictives pour simuler une exécution.
- Une déclaration de validation GNS3 sans test réel.

## 9. Compte rendu obligatoire

À la fin, indique :

1. L’état du TUI avant modification : bibliothèques réellement utilisées et architecture constatée.
2. Les fonctionnalités web et TUI ajoutées ou corrigées.
3. Les fichiers modifiés et les raisons de chaque modification.
4. Les commandes de tests exécutées et leurs résultats réels.
5. Les éventuelles limitations et les tests reportés à la campagne finale.
6. La prochaine tâche recommandée.

**Critère de fin :** les administrateurs peuvent consulter et suivre les exécutions réelles depuis le dashboard web et le TUI Go interactif utilisant Lip Gloss et l’écosystème Charmbracelet, examiner les résultats et demander une annulation sans contourner le Provider, ses autorisations ou son audit.