// The app's own sticker packs (phase 5.6). The art is part of the build, so
// it's as verifiable as the code, and sending one is just its name: nothing
// is uploaded, and nothing outside the app is asked for it (no Telegram
// packs: a Mini App can't reach them, and fetching them through the bot
// would show who uses which). The images are Microsoft's Fluent Emoji (3D),
// MIT licensed (art/NOTICE.md, ART_LICENSE); art/make-art.sh remakes them
// from the source.
//
// Sticker IDs ("pack/name") travel in messages: never rename or remove one.

/** A sticker: its ID and the emoji it stands for. */
export interface StickerInfo {
  id: string;
  e: string;
}

export interface Pack {
  id: string;
  stickers: readonly StickerInfo[];
}

const pack = (id: string, stickers: [string, string][]): Pack => ({ id, stickers: stickers.map(([name, e]) => ({ id: `${id}/${name}`, e })) });

export const PACKS: readonly Pack[] = [
  pack('faces', [
    ['grinning_face_with_big_eyes', '😃'],
    ['beaming_face_with_smiling_eyes', '😁'],
    ['face_with_tears_of_joy', '😂'],
    ['rolling_on_the_floor_laughing', '🤣'],
    ['smiling_face_with_heart_eyes', '😍'],
    ['face_blowing_a_kiss', '😘'],
    ['winking_face', '😉'],
    ['smiling_face_with_sunglasses', '😎'],
    ['star_struck', '🤩'],
    ['partying_face', '🥳'],
    ['thinking_face', '🤔'],
    ['face_with_rolling_eyes', '🙄'],
    ['smirking_face', '😏'],
    ['pleading_face', '🥺'],
    ['loudly_crying_face', '😭'],
    ['pouting_face', '😡'],
    ['face_with_steam_from_nose', '😤'],
    ['exploding_head', '🤯'],
    ['face_screaming_in_fear', '😱'],
    ['sleeping_face', '😴'],
    ['nauseated_face', '🤢'],
    ['shushing_face', '🤫'],
    ['hugging_face', '🤗'],
    ['saluting_face', '🫡'],
  ]),
  pack('love', [
    ['red_heart', '❤️'],
    ['broken_heart', '💔'],
    ['sparkling_heart', '💖'],
    ['two_hearts', '💕'],
    ['fire', '🔥'],
    ['sparkles', '✨'],
    ['hundred_points', '💯'],
    ['thumbs_up', '👍'],
    ['thumbs_down', '👎'],
    ['clapping_hands', '👏'],
    ['folded_hands', '🙏'],
    ['waving_hand', '👋'],
    ['ok_hand', '👌'],
    ['victory_hand', '✌️'],
    ['flexed_biceps', '💪'],
    ['raising_hands', '🙌'],
    ['handshake', '🤝'],
    ['crossed_fingers', '🤞'],
    ['party_popper', '🎉'],
    ['trophy', '🏆'],
    ['rocket', '🚀'],
    ['light_bulb', '💡'],
    ['check_mark_button', '✅'],
    ['birthday_cake', '🎂'],
  ]),
  pack('animals', [
    ['cat_face', '🐱'],
    ['dog_face', '🐶'],
    ['fox', '🦊'],
    ['panda', '🐼'],
    ['koala', '🐨'],
    ['see_no_evil_monkey', '🙈'],
    ['hear_no_evil_monkey', '🙉'],
    ['speak_no_evil_monkey', '🙊'],
    ['unicorn', '🦄'],
    ['frog', '🐸'],
    ['penguin', '🐧'],
    ['owl', '🦉'],
    ['bear', '🐻'],
    ['pig_face', '🐷'],
    ['rabbit_face', '🐰'],
    ['tiger_face', '🐯'],
    ['lion', '🦁'],
    ['hamster', '🐹'],
    ['turtle', '🐢'],
    ['octopus', '🐙'],
    ['butterfly', '🦋'],
    ['snail', '🐌'],
    ['sloth', '🦥'],
    ['t_rex', '🦖'],
  ]),
];

const BY_ID = new Map(PACKS.flatMap((p) => p.stickers.map((s) => [s.id, s] as const)));

/** A sticker this version has, by ID. */
export function findSticker(id: string): StickerInfo | undefined {
  return BY_ID.get(id);
}

/** The sticker images' license, shown in the sticker panel (it must ship with them). */
export const ART_CREDIT = 'Fluent Emoji © Microsoft Corporation · MIT License';
export const ART_LICENSE = `MIT License

Copyright (c) Microsoft Corporation.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE`;

let loading: Promise<Readonly<Record<string, string>>> | null = null;

/**
 * The sticker images, as object URLs by ID: all of them, fetched once, when
 * a conversation opens rather than when a sticker shows, so the fetches say
 * nothing about stickers being sent or received. If any can't be fetched
 * (a deploy replaced them since this page loaded), stickers show as their
 * emoji until the next try.
 */
export function loadArt(): Promise<Readonly<Record<string, string>>> {
  loading ??= import('./art')
    .then(({ ART }) =>
      Promise.all(
        Object.entries(ART).map(async ([id, url]) => {
          const response = await fetch(url);
          if (!response.ok) throw new Error(`sticker ${response.status}`);
          return [id, URL.createObjectURL(await response.blob())] as const;
        }),
      ),
    )
    .then(Object.fromEntries<string>, (err: unknown) => {
      loading = null;
      throw err;
    });
  return loading;
}
