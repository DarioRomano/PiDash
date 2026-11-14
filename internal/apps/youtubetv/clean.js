// Part of PiDash. Runs inside YouTube's TV interface, before YouTube's own
// code, and takes the advertising out of what YouTube sends.
//
// The interface asks YouTube for everything it shows (what to play, what is
// on the home page) and gets its answers as JSON text. Those answers say
// which adverts to play before and during a video, and carry adverts as
// entries among the rows of videos. Here every answer is looked at as it is
// read, and those parts are left out. The interface then simply has no
// advert to show. Nothing is blocked on the network, and nothing about the
// videos themselves is touched.
(() => {
  "use strict";
  if (window.__pidashClean) return;
  window.__pidashClean = true;

  const read = JSON.parse;

  // In an answer about a video: which adverts to play, and when.
  const AD_PLANS = ["adPlacements", "adSlots", "playerAds", "adBreakHeartbeatParams"];
  // In a list: entries that are an advert rather than a video. The first is a
  // slot in a row of videos, the second the banner at the top of the home page.
  const AD_ENTRIES = ["adSlotRenderer", "tvMastheadRenderer"];

  function withoutPlans(answer) {
    if (!answer || typeof answer !== "object") return;
    for (const plan of AD_PLANS) if (plan in answer) delete answer[plan];
  }

  const isAd = (entry) => entry !== null && typeof entry === "object" && AD_ENTRIES.some((kind) => kind in entry);

  // Goes through an answer and takes advert entries out of every list in it.
  // The depth is limited so that nothing YouTube could send keeps this busy.
  function withoutAdEntries(node, depth) {
    if (node === null || typeof node !== "object" || depth > 40) return;
    if (Array.isArray(node)) {
      for (let i = node.length - 1; i >= 0; i--) {
        if (isAd(node[i])) node.splice(i, 1);
        else withoutAdEntries(node[i], depth + 1);
      }
      return;
    }
    for (const key in node) withoutAdEntries(node[key], depth + 1);
  }

  JSON.parse = function (text, reviver) {
    const answer = read.call(this, text, reviver);
    // Whatever goes wrong in here, YouTube gets its answer as it was.
    try {
      if (answer !== null && typeof answer === "object" && !Array.isArray(answer) && typeof text === "string" && text.length > 200) {
        withoutPlans(answer);
        withoutPlans(answer.playerResponse);
        // Only answers from YouTube's own service are searched for entries:
        // they all say in what context they were made.
        if (answer.responseContext || answer.contents || answer.continuationContents) withoutAdEntries(answer, 0);
      }
    } catch {
      /* left as it was */
    }
    return answer;
  };
})();
