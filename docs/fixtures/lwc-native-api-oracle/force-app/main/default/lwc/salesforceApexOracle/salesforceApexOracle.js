import { LightningElement } from "lwc";
import value from "@salesforce/apex/GladeLwcOracleController.ping";

export default class Oracle extends LightningElement {
  label = "@salesforce/apex/";
  value = value;
}
